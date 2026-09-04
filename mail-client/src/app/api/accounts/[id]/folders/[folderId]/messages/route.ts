import { NextRequest, NextResponse } from "next/server";
import { and, desc, eq } from "drizzle-orm";
import { db } from "@/db";
import { folders, messages } from "@/db/schema";
import { requireAccount, UnauthorizedError, NotFoundError } from "@/lib/require-account";

export async function GET(
  req: NextRequest,
  { params }: { params: Promise<{ id: string; folderId: string }> },
) {
  const { id, folderId } = await params;
  try {
    const account = await requireAccount(id);

    const [folder] = await db
      .select()
      .from(folders)
      .where(and(eq(folders.id, folderId), eq(folders.accountId, account.id)))
      .limit(1);
    if (!folder) return NextResponse.json({ error: "folder not found" }, { status: 404 });

    const limit = Math.min(Number(req.nextUrl.searchParams.get("limit") ?? 50), 200);
    const offset = Math.max(Number(req.nextUrl.searchParams.get("offset") ?? 0), 0);

    const rows = await db
      .select({
        id: messages.id,
        uid: messages.uid,
        subject: messages.subject,
        fromName: messages.fromName,
        fromAddress: messages.fromAddress,
        date: messages.date,
        snippet: messages.snippet,
        seen: messages.seen,
        flagged: messages.flagged,
        hasAttachments: messages.hasAttachments,
      })
      .from(messages)
      .where(eq(messages.folderId, folder.id))
      .orderBy(desc(messages.date))
      .limit(limit)
      .offset(offset);

    return NextResponse.json({ folder, messages: rows });
  } catch (err) {
    if (err instanceof UnauthorizedError) return NextResponse.json({ error: "not logged in" }, { status: 401 });
    if (err instanceof NotFoundError) return NextResponse.json({ error: "not found" }, { status: 404 });
    throw err;
  }
}
