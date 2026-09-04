import { NextRequest, NextResponse } from "next/server";
import { and, eq } from "drizzle-orm";
import { db } from "@/db";
import { accounts, folders, messages } from "@/db/schema";
import { fetchMessageBody, setMessageFlag } from "@/lib/imap";
import { requireSession, UnauthorizedError } from "@/lib/require-account";

async function loadOwnedMessage(msgId: string, profileId: string) {
  const [row] = await db
    .select({
      message: messages,
      folder: folders,
      account: accounts,
    })
    .from(messages)
    .innerJoin(folders, eq(folders.id, messages.folderId))
    .innerJoin(accounts, eq(accounts.id, messages.accountId))
    .where(and(eq(messages.id, msgId), eq(accounts.profileId, profileId)))
    .limit(1);
  return row ?? null;
}

export async function GET(_req: NextRequest, { params }: { params: Promise<{ msgId: string }> }) {
  const { msgId } = await params;
  try {
    const session = await requireSession();
    const row = await loadOwnedMessage(msgId, session.profileId);
    if (!row) return NextResponse.json({ error: "not found" }, { status: 404 });

    let { message } = row;

    if (!message.bodyFetched) {
      const body = await fetchMessageBody(row.account, row.folder.name, message.uid);
      const snippet = (body.bodyText ?? "").replace(/\s+/g, " ").trim().slice(0, 160) || null;
      const [updated] = await db
        .update(messages)
        .set({
          bodyText: body.bodyText,
          bodyHtml: body.bodyHtml,
          attachments: body.attachments,
          hasAttachments: body.attachments.length > 0,
          bodyFetched: true,
          snippet,
        })
        .where(eq(messages.id, message.id))
        .returning();
      message = updated;
    }

    if (!message.seen) {
      await db.update(messages).set({ seen: true }).where(eq(messages.id, message.id));
      setMessageFlag(row.account, row.folder.name, message.uid, "\\Seen", true).catch(() => {});
      message = { ...message, seen: true };
    }

    return NextResponse.json({ message });
  } catch (err) {
    if (err instanceof UnauthorizedError) return NextResponse.json({ error: "not logged in" }, { status: 401 });
    throw err;
  }
}
