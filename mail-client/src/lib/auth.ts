import { SignJWT, jwtVerify } from "jose";
import { cookies } from "next/headers";
import { randomUUID, createHash } from "crypto";
import { eq } from "drizzle-orm";
import { db } from "@/db";
import { profiles, sessions } from "@/db/schema";
import { ensureSecretKey } from "./secretkey";

export const SESSION_COOKIE = "patrabahok_mail_session";
const SESSION_TTL_SECONDS = 30 * 24 * 60 * 60; // 30 days

function signingKey() {
  return ensureSecretKey("MAIL_CLIENT_SESSION_KEY");
}

function hashToken(token: string): string {
  return createHash("sha256").update(token).digest("hex");
}

/** Creates a new profile (first login ever for this browser) and its first session. */
export async function createProfileAndSession(): Promise<string> {
  const [profile] = await db.insert(profiles).values({}).returning();
  return issueSession(profile.id);
}

/** Issues a new session for an existing profile (e.g. adding a 2nd+ account). */
export async function issueSession(profileId: string): Promise<string> {
  const sessionId = randomUUID();
  const expiresAt = new Date(Date.now() + SESSION_TTL_SECONDS * 1000);

  const jwt = await new SignJWT({ sid: sessionId, pid: profileId })
    .setProtectedHeader({ alg: "HS256" })
    .setIssuedAt()
    .setExpirationTime(Math.floor(expiresAt.getTime() / 1000))
    .sign(signingKey());

  await db.insert(sessions).values({
    id: sessionId,
    profileId,
    tokenHash: hashToken(jwt),
    expiresAt,
  });

  const jar = await cookies();
  jar.set(SESSION_COOKIE, jwt, {
    httpOnly: true,
    secure: true,
    sameSite: "lax",
    path: "/",
    maxAge: SESSION_TTL_SECONDS,
  });

  return profileId;
}

export interface CurrentSession {
  profileId: string;
  sessionId: string;
}

/** Verifies the session cookie's signature/expiry AND that its DB row still exists
 * (so logout / expiry cleanup actually revokes it, not just relying on JWT expiry). */
export async function getCurrentSession(): Promise<CurrentSession | null> {
  const jar = await cookies();
  const token = jar.get(SESSION_COOKIE)?.value;
  if (!token) return null;

  let payload: { sid?: string; pid?: string };
  try {
    const verified = await jwtVerify(token, signingKey());
    payload = verified.payload as { sid?: string; pid?: string };
  } catch {
    return null;
  }
  if (!payload.sid || !payload.pid) return null;

  const [row] = await db
    .select({ id: sessions.id, expiresAt: sessions.expiresAt })
    .from(sessions)
    .where(eq(sessions.id, payload.sid))
    .limit(1);
  if (!row || row.expiresAt.getTime() < Date.now()) return null;

  return { profileId: payload.pid, sessionId: payload.sid };
}

export async function destroySession(): Promise<void> {
  const jar = await cookies();
  const token = jar.get(SESSION_COOKIE)?.value;
  jar.delete(SESSION_COOKIE);
  if (!token) return;
  try {
    const { payload } = await jwtVerify(token, signingKey());
    const sid = (payload as { sid?: string }).sid;
    if (sid) {
      await db.delete(sessions).where(eq(sessions.id, sid));
    }
  } catch {
    // token already invalid — nothing to clean up
  }
}
