import { eq, and } from "drizzle-orm";
import { db } from "@/db";
import { accounts } from "@/db/schema";
import { getCurrentSession } from "./auth";

export class UnauthorizedError extends Error {}
export class NotFoundError extends Error {}

/** Loads an account by id, but only if it belongs to the current session's profile —
 * the authorization boundary for every per-account API route. */
export async function requireAccount(accountId: string) {
  const session = await getCurrentSession();
  if (!session) throw new UnauthorizedError("not logged in");

  const [account] = await db
    .select()
    .from(accounts)
    .where(and(eq(accounts.id, accountId), eq(accounts.profileId, session.profileId)))
    .limit(1);
  if (!account) throw new NotFoundError("account not found");
  return account;
}

export async function requireSession() {
  const session = await getCurrentSession();
  if (!session) throw new UnauthorizedError("not logged in");
  return session;
}
