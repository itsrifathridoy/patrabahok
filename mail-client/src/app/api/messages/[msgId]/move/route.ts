import { NextRequest, NextResponse } from "next/server";
import { and, eq } from "drizzle-orm";
import { db } from "@/db";
import { accounts, folders, messages } from "@/db/schema";
import { moveMessage } from "@/lib/imap";
import { requireSession, UnauthorizedError } from "@/lib/require-account";

/** Moves a message to a folder identified by role ("trash"/"archive"/etc.), the way the
 * UI's Archive/Delete buttons work — no need for the client to know folder IDs. */
export async function POST(req: NextRequest, { params }: { params: Promise<{ msgId: string }> }) {
  const { msgId } = await params;
  try {
    const session = await requireSession();
    const body = await req.json().catch(() => null);
    const toRole = typeof body?.toRole === "string" ? body.toRole : null;
    if (!toRole) return NextResponse.json({ error: "toRole is required" }, { status: 400 });

    const [row] = await db
      .select({ message: messages, folder: folders, account: accounts })
      .from(messages)
      .innerJoin(folders, eq(folders.id, messages.folderId))
      .innerJoin(accounts, eq(accounts.id, messages.accountId))
      .where(and(eq(messages.id, msgId), eq(accounts.profileId, session.profileId)))
      .limit(1);
    if (!row) return NextResponse.json({ error: "not found" }, { status: 404 });

    const [targetFolder] = await db
      .select()
      .from(folders)
      .where(and(eq(folders.accountId, row.account.id), eq(folders.role, toRole)))
      .limit(1);
    if (!targetFolder) return NextResponse.json({ error: `no '${toRole}' folder for this account` }, { status: 400 });

    await moveMessage(row.account, row.folder.name, row.message.uid, targetFolder.name);
    await db.delete(messages).where(eq(messages.id, msgId));

    return NextResponse.json({ ok: true });
  } catch (err) {
    if (err instanceof UnauthorizedError) return NextResponse.json({ error: "not logged in" }, { status: 401 });
    throw err;
  }
}
