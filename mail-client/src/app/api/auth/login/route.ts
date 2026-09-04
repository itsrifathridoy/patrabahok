import { NextRequest, NextResponse } from "next/server";
import { eq, and, sql } from "drizzle-orm";
import { db } from "@/db";
import { accounts } from "@/db/schema";
import { verifyImapLogin, ensureFoldersSynced, syncFolder } from "@/lib/imap";
import { encryptSecret } from "@/lib/crypto";
import { createProfileAndSession, issueSession, getCurrentSession } from "@/lib/auth";
import { IMAP_HOST, IMAP_PORT, SMTP_HOST, SMTP_PORT } from "@/lib/mailserver";

export async function POST(req: NextRequest) {
  const body = await req.json().catch(() => null);
  const email = typeof body?.email === "string" ? body.email.trim().toLowerCase() : "";
  const password = typeof body?.password === "string" ? body.password : "";
  if (!email || !password) {
    return NextResponse.json({ error: "Email and password are required." }, { status: 400 });
  }

  try {
    await verifyImapLogin(email, password);
  } catch {
    return NextResponse.json({ error: "Incorrect email or password." }, { status: 401 });
  }

  // Logged in already (adding a 2nd+ account) vs. brand new browser session.
  const existingSession = await getCurrentSession();
  const profileId = existingSession
    ? existingSession.profileId
    : await createProfileAndSession();
  if (existingSession) {
    // Refresh the cookie's expiry even when reusing the existing profile.
    await issueSession(profileId);
  }

  const [existingAccount] = await db
    .select()
    .from(accounts)
    .where(and(eq(accounts.profileId, profileId), eq(accounts.email, email)))
    .limit(1);

  let account = existingAccount;
  if (!account) {
    const [{ count }] = await db
      .select({ count: sql<number>`count(*)::int` })
      .from(accounts)
      .where(eq(accounts.profileId, profileId));
    const colors = ["coral", "mint", "lavender"];

    const [created] = await db
      .insert(accounts)
      .values({
        profileId,
        email,
        username: email,
        imapHost: IMAP_HOST,
        imapPort: IMAP_PORT,
        smtpHost: SMTP_HOST,
        smtpPort: SMTP_PORT,
        passwordEncrypted: encryptSecret(password),
        colorTag: colors[count % colors.length],
      })
      .returning();
    account = created;
  } else {
    // Password may have changed since it was first connected — keep it current.
    await db
      .update(accounts)
      .set({ passwordEncrypted: encryptSecret(password) })
      .where(eq(accounts.id, account.id));
  }

  // First-login setup happens inline (folders + Inbox) so the UI has something to show
  // immediately; other folders sync afterward via the client's periodic sync calls.
  const folderRows = await ensureFoldersSynced(account);
  const inbox = folderRows.find((f) => f.role === "inbox");
  if (inbox) {
    await syncFolder(account, inbox);
  }

  return NextResponse.json({ accountId: account.id });
}
