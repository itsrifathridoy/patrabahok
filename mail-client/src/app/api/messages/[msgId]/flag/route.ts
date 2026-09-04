import { NextRequest, NextResponse } from "next/server";
import { and, eq } from "drizzle-orm";
import { db } from "@/db";
import { accounts, folders, messages } from "@/db/schema";
import { setMessageFlag } from "@/lib/imap";
import { requireSession, UnauthorizedError } from "@/lib/require-account";

export async function POST(req: NextRequest, { params }: { params: Promise<{ msgId: string }> }) {
  const { msgId } = await params;
  try {
    const session = await requireSession();
    const body = await req.json().catch(() => null);
    const flag = body?.flag === "flagged" ? "\\Flagged" : body?.flag === "seen" ? "\\Seen" : null;
    const value = Boolean(body?.value);
    if (!flag) return NextResponse.json({ error: "flag must be 'seen' or 'flagged'" }, { status: 400 });

    const [row] = await db
      .select({ message: messages, folder: folders, account: accounts })
      .from(messages)
      .innerJoin(folders, eq(folders.id, messages.folderId))
      .innerJoin(accounts, eq(accounts.id, messages.accountId))
      .where(and(eq(messages.id, msgId), eq(accounts.profileId, session.profileId)))
      .limit(1);
    if (!row) return NextResponse.json({ error: "not found" }, { status: 404 });

    const column = flag === "\\Seen" ? { seen: value } : { flagged: value };
    await db.update(messages).set(column).where(eq(messages.id, msgId));
    await setMessageFlag(row.account, row.folder.name, row.message.uid, flag, value);

    return NextResponse.json({ ok: true });
  } catch (err) {
    if (err instanceof UnauthorizedError) return NextResponse.json({ error: "not logged in" }, { status: 401 });
    throw err;
  }
}
