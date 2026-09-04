import { NextResponse } from "next/server";
import { eq } from "drizzle-orm";
import { db } from "@/db";
import { accounts } from "@/db/schema";
import { getCurrentSession } from "@/lib/auth";

export async function GET() {
  const session = await getCurrentSession();
  if (!session) return NextResponse.json({ error: "not logged in" }, { status: 401 });

  const rows = await db
    .select({
      id: accounts.id,
      email: accounts.email,
      displayName: accounts.displayName,
      colorTag: accounts.colorTag,
      lastSyncedAt: accounts.lastSyncedAt,
      lastSyncError: accounts.lastSyncError,
    })
    .from(accounts)
    .where(eq(accounts.profileId, session.profileId))
    .orderBy(accounts.createdAt);

  return NextResponse.json({ accounts: rows });
}
