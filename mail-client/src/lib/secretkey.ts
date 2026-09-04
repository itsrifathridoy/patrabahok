import { readFileSync, appendFileSync, existsSync, mkdirSync, chmodSync } from "fs";
import { randomBytes } from "crypto";

// Mirrors cli/internal/secretkey (Go): keys used to encrypt sensitive values this app
// stores in Postgres (mailbox passwords) live in /etc/patrabahok/secrets.env — the same
// root-only (0600) file the installer and admin dashboard already use — not in the
// database, so a database-only compromise doesn't hand over live mailbox credentials.
const SECRETS_FILE = "/etc/patrabahok/secrets.env";

const cache = new Map<string, Buffer>();

export function ensureSecretKey(name: string): Buffer {
  const cached = cache.get(name);
  if (cached) return cached;

  const existing = readSecret(name);
  if (existing) {
    cache.set(name, existing);
    return existing;
  }

  const raw = randomBytes(32);
  const encoded = raw.toString("base64");

  mkdirSync("/etc/patrabahok", { recursive: true, mode: 0o700 });
  appendFileSync(SECRETS_FILE, `${name}=${encoded}\n`, { mode: 0o600 });
  try {
    chmodSync(SECRETS_FILE, 0o600);
  } catch {
    // best-effort; file was created with 0o600 above already if it didn't exist
  }

  // Re-read rather than trusting our own write, in case a concurrent process won the
  // race and appended its own value first.
  const confirmed = readSecret(name);
  if (!confirmed) {
    throw new Error(`wrote ${name} to ${SECRETS_FILE} but could not read it back`);
  }
  cache.set(name, confirmed);
  return confirmed;
}

function readSecret(name: string): Buffer | null {
  if (!existsSync(SECRETS_FILE)) return null;
  const content = readFileSync(SECRETS_FILE, "utf8");
  const prefix = `${name}=`;
  let last: string | null = null;
  for (const line of content.split("\n")) {
    if (line.startsWith(prefix)) {
      last = line.slice(prefix.length).trim();
    }
  }
  if (!last) return null;
  return Buffer.from(last, "base64");
}
