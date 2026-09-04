import { ImapFlow, type FetchMessageObject } from "imapflow";
import { simpleParser } from "mailparser";
import { eq, sql } from "drizzle-orm";
import { db } from "@/db";
import { folders, messages, type accounts as AccountsTable } from "@/db/schema";
import { decryptSecret } from "./crypto";
import { IMAP_HOST, IMAP_PORT } from "./mailserver";

type Account = typeof AccountsTable.$inferSelect;

const STANDARD_FOLDERS: { name: string; role: string }[] = [
  { name: "Sent", role: "sent" },
  { name: "Drafts", role: "drafts" },
  { name: "Trash", role: "trash" },
  { name: "Archive", role: "archive" },
  // Matches the Dovecot global sieve rule (templates/dovecot/sieve/spam-to-junk.sieve)
  // that files Rspamd-flagged mail here via `fileinto :create "Junk"` — creating it
  // here too means it shows up in the sidebar immediately, not just after the first
  // spam message arrives.
  { name: "Junk", role: "junk" },
];

const INITIAL_SYNC_LIMIT = 200;

function openClient(email: string, password: string): ImapFlow {
  return new ImapFlow({
    host: IMAP_HOST,
    port: IMAP_PORT,
    secure: true,
    auth: { user: email, pass: password },
    logger: false,
  });
}

/** Verifies credentials by actually logging in — the source of truth for whether an
 * account can be added is a real IMAP LOGIN against Dovecot, not a separate password
 * check of our own. */
export async function verifyImapLogin(email: string, password: string): Promise<void> {
  const client = openClient(email, password);
  await client.connect();
  await client.logout();
}

async function withClient<T>(
  email: string,
  passwordEncrypted: string,
  fn: (client: ImapFlow) => Promise<T>,
): Promise<T> {
  const client = openClient(email, decryptSecret(passwordEncrypted));
  await client.connect();
  try {
    return await fn(client);
  } finally {
    await client.logout().catch(() => client.close());
  }
}

function roleForMailbox(path: string, specialUse?: string | false): string | null {
  if (specialUse) {
    const m = specialUse.replace("\\", "").toLowerCase();
    if (m === "sent" || m === "drafts" || m === "trash" || m === "archive" || m === "junk") return m;
  }
  const lower = path.toLowerCase();
  if (lower === "inbox") return "inbox";
  if (lower.includes("sent")) return "sent";
  if (lower.includes("draft")) return "drafts";
  if (lower.includes("trash") || lower.includes("deleted")) return "trash";
  if (lower.includes("archive")) return "archive";
  if (lower.includes("junk") || lower.includes("spam")) return "junk";
  return null;
}

/** Ensures INBOX + the standard Sent/Drafts/Trash/Archive folders exist for this
 * account (freshly created mailboxes only ever have INBOX — see
 * templates/dovecot/99-patrabahok.conf.tmpl), and returns the folders known to Postgres
 * afterward (creating rows for any newly discovered mailbox). */
export async function ensureFoldersSynced(account: Account): Promise<(typeof folders.$inferSelect)[]> {
  return withClient(account.email, account.passwordEncrypted, async (client) => {
    const existing = await client.list();
    const existingPaths = new Set(existing.map((m) => m.path));

    let createdAny = false;
    for (const std of STANDARD_FOLDERS) {
      if (!existingPaths.has(std.name)) {
        await client.mailboxCreate(std.name);
        createdAny = true;
      }
    }

    const finalList = createdAny ? await client.list() : existing;

    const rows: (typeof folders.$inferSelect)[] = [];
    for (const mbox of finalList) {
      if (mbox.flags?.has("\\Noselect")) continue;
      const role = roleForMailbox(mbox.path, mbox.specialUse);
      const [row] = await db
        .insert(folders)
        .values({ accountId: account.id, name: mbox.path, displayName: mbox.name, role })
        .onConflictDoUpdate({
          target: [folders.accountId, folders.name],
          set: { displayName: mbox.name, role },
        })
        .returning();
      rows.push(row);
    }
    return rows;
  });
}

export interface SyncResult {
  folder: string;
  newMessages: number;
  error?: string;
}

/** Incrementally syncs one folder: fetches envelopes/flags for UIDs newer than the last
 * one seen (or the most recent INITIAL_SYNC_LIMIT on first sync) and upserts them —
 * message bodies are fetched lazily on open, not here, so this stays fast. */
export async function syncFolder(
  account: Account,
  folder: typeof folders.$inferSelect,
): Promise<SyncResult> {
  try {
    return await withClient(account.email, account.passwordEncrypted, async (client) => {
      const lock = await client.getMailboxLock(folder.name);
      let newCount = 0;
      try {
        const mailbox = client.mailbox;
        if (!mailbox || typeof mailbox === "boolean") return { folder: folder.name, newMessages: 0 };

        const uidValidity = mailbox.uidValidity.toString();
        let sinceUid = folder.lastSeenUid;
        if (folder.uidValidity && folder.uidValidity !== uidValidity) {
          // The server renumbered UIDs (rare, but must be handled) — full resync.
          sinceUid = 0;
        }

        let maxUid = sinceUid;

        if (sinceUid > 0) {
          // Incremental: fetch only UIDs newer than the last one we saw. IMAP servers
          // normalize a "N:*" range where N exceeds every existing UID into "highest:N"
          // (RFC 3501's "smaller number first" range rule), which re-matches the
          // newest already-seen message when there's nothing actually new — filter
          // those back out rather than re-fetching/upserting them for no reason.
          const rawFound = (await client.search({ uid: `${sinceUid + 1}:*` }, { uid: true })) as number[] | false;
          const found = rawFound ? rawFound.filter((uid) => uid > sinceUid) : [];
          if (found.length > 0) {
            for await (const msg of client.fetch(
              found,
              { uid: true, envelope: true, flags: true, bodyStructure: true, size: true },
              { uid: true },
            )) {
              await upsertMessage(account.id, folder.id, msg);
              if (msg.uid > maxUid) maxUid = msg.uid;
              newCount++;
            }
          }
        } else if (mailbox.exists > 0) {
          // First sync for this folder: just the most recent INITIAL_SYNC_LIMIT messages.
          const start = Math.max(1, mailbox.exists - INITIAL_SYNC_LIMIT + 1);
          for await (const msg of client.fetch(
            `${start}:*`,
            { uid: true, envelope: true, flags: true, bodyStructure: true, size: true },
          )) {
            await upsertMessage(account.id, folder.id, msg);
            if (msg.uid > maxUid) maxUid = msg.uid;
            newCount++;
          }
        }

        const counts = await db
          .select({
            total: sql<number>`count(*)::int`,
            unread: sql<number>`count(*) filter (where not seen)::int`,
          })
          .from(messages)
          .where(eq(messages.folderId, folder.id));

        await db
          .update(folders)
          .set({
            lastSeenUid: maxUid,
            uidValidity,
            totalCount: counts[0]?.total ?? 0,
            unreadCount: counts[0]?.unread ?? 0,
          })
          .where(eq(folders.id, folder.id));
      } finally {
        lock.release();
      }
      return { folder: folder.name, newMessages: newCount };
    });
  } catch (err) {
    return { folder: folder.name, newMessages: 0, error: (err as Error).message };
  }
}

async function upsertMessage(accountId: string, folderId: string, msg: FetchMessageObject) {
  const env = msg.envelope;
  const bs = msg.bodyStructure;
  const hasAttachments = bodyStructureHasAttachments(bs);

  await db
    .insert(messages)
    .values({
      accountId,
      folderId,
      uid: msg.uid,
      messageIdHeader: env?.messageId ?? null,
      inReplyTo: env?.inReplyTo ?? null,
      subject: env?.subject ?? null,
      fromName: env?.from?.[0]?.name ?? null,
      fromAddress: env?.from?.[0]?.address ?? null,
      toAddresses: env?.to?.map((a) => ({ name: a.name ?? "", address: a.address ?? "" })) ?? [],
      ccAddresses: env?.cc?.map((a) => ({ name: a.name ?? "", address: a.address ?? "" })) ?? [],
      date: env?.date ?? null,
      seen: msg.flags?.has("\\Seen") ?? false,
      flagged: msg.flags?.has("\\Flagged") ?? false,
      hasAttachments,
      sizeBytes: msg.size ?? null,
    })
    .onConflictDoUpdate({
      target: [messages.folderId, messages.uid],
      set: {
        seen: msg.flags?.has("\\Seen") ?? false,
        flagged: msg.flags?.has("\\Flagged") ?? false,
      },
    });
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
function bodyStructureHasAttachments(bs: any): boolean {
  if (!bs) return false;
  if (bs.disposition && bs.disposition.toLowerCase() === "attachment") return true;
  if (Array.isArray(bs.childNodes)) {
    return bs.childNodes.some(bodyStructureHasAttachments);
  }
  return false;
}

/** Fetches and parses the full message body (text/html/attachments), on demand, the
 * first time a message is opened — keeps folder sync fast by not downloading full
 * bodies up front. */
export async function fetchMessageBody(
  account: Account,
  folderName: string,
  uid: number,
): Promise<{ bodyText: string | null; bodyHtml: string | null; attachments: { filename: string; sizeBytes: number; contentType: string; partId: string }[] }> {
  return withClient(account.email, account.passwordEncrypted, async (client) => {
    const lock = await client.getMailboxLock(folderName);
    try {
      const raw = await client.download(uid, undefined, { uid: true });
      const chunks: Buffer[] = [];
      for await (const chunk of raw.content) chunks.push(chunk as Buffer);
      const parsed = await simpleParser(Buffer.concat(chunks));

      const attachments = (parsed.attachments ?? []).map((a, i) => ({
        filename: a.filename ?? `attachment-${i}`,
        sizeBytes: a.size ?? 0,
        contentType: a.contentType ?? "application/octet-stream",
        partId: String(i),
      }));

      return {
        bodyText: parsed.text ?? null,
        bodyHtml: typeof parsed.html === "string" ? parsed.html : null,
        attachments,
      };
    } finally {
      lock.release();
    }
  });
}

export async function setMessageFlag(
  account: Account,
  folderName: string,
  uid: number,
  flag: "\\Seen" | "\\Flagged",
  value: boolean,
): Promise<void> {
  await withClient(account.email, account.passwordEncrypted, async (client) => {
    const lock = await client.getMailboxLock(folderName);
    try {
      if (value) {
        await client.messageFlagsAdd({ uid: String(uid) }, [flag], { uid: true });
      } else {
        await client.messageFlagsRemove({ uid: String(uid) }, [flag], { uid: true });
      }
    } finally {
      lock.release();
    }
  });
}

/** Appends a raw RFC 822 message to a mailbox — used to save a copy of a just-sent
 * message into the Sent folder, since SMTP submission alone doesn't do that. */
export async function appendMessage(
  account: Account,
  folderName: string,
  raw: Buffer,
  flags: string[] = ["\\Seen"],
): Promise<void> {
  await withClient(account.email, account.passwordEncrypted, async (client) => {
    await client.append(folderName, raw, flags);
  });
}

export async function moveMessage(
  account: Account,
  fromFolder: string,
  uid: number,
  toFolder: string,
): Promise<void> {
  await withClient(account.email, account.passwordEncrypted, async (client) => {
    const lock = await client.getMailboxLock(fromFolder);
    try {
      await client.messageMove({ uid: String(uid) }, toFolder, { uid: true });
    } finally {
      lock.release();
    }
  });
}
