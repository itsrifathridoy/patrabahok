import { NextRequest, NextResponse } from "next/server";
import { eq } from "drizzle-orm";
import { db } from "@/db";
import { accounts, folders } from "@/db/schema";
import { syncFolder } from "@/lib/imap";
import { requireAccount, UnauthorizedError, NotFoundError } from "@/lib/require-account";

export async function POST(_req: NextRequest, { params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  try {
    const account = await requireAccount(id);
    const folderRows = await db.select().from(folders).where(eq(folders.accountId, account.id));

    const results = [];
    let anyError: string | null = null;
    for (const folder of folderRows) {
      const result = await syncFolder(account, folder);
      results.push(result);
      if (result.error) anyError = result.error;
    }

    await db
      .update(accounts)
      .set({ lastSyncedAt: new Date(), lastSyncError: anyError })
      .where(eq(accounts.id, account.id));

    return NextResponse.json({ results });
  } catch (err) {
    if (err instanceof UnauthorizedError) return NextResponse.json({ error: "not logged in" }, { status: 401 });
    if (err instanceof NotFoundError) return NextResponse.json({ error: "not found" }, { status: 404 });
    throw err;
  }
}
