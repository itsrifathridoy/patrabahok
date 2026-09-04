import { NextRequest, NextResponse } from "next/server";
import { eq } from "drizzle-orm";
import { db } from "@/db";
import { folders } from "@/db/schema";
import { requireAccount, UnauthorizedError, NotFoundError } from "@/lib/require-account";

export async function GET(_req: NextRequest, { params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  try {
    const account = await requireAccount(id);
    const rows = await db.select().from(folders).where(eq(folders.accountId, account.id));
    // Stable, predictable order: Inbox first, standard roles next, everything else after.
    const order: Record<string, number> = { inbox: 0, sent: 1, drafts: 2, archive: 3, junk: 4, trash: 5 };
    rows.sort((a, b) => (order[a.role ?? ""] ?? 99) - (order[b.role ?? ""] ?? 99) || a.displayName.localeCompare(b.displayName));
    return NextResponse.json({ folders: rows });
  } catch (err) {
    if (err instanceof UnauthorizedError) return NextResponse.json({ error: "not logged in" }, { status: 401 });
    if (err instanceof NotFoundError) return NextResponse.json({ error: "not found" }, { status: 404 });
    throw err;
  }
}
