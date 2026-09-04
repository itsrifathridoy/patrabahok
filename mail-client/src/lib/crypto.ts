import { createCipheriv, createDecipheriv, randomBytes } from "crypto";
import { ensureSecretKey } from "./secretkey";

const KEY_NAME = "MAIL_CLIENT_ENC_KEY";
const ALGO = "aes-256-gcm";

export function encryptSecret(plaintext: string): string {
  const key = ensureSecretKey(KEY_NAME);
  const iv = randomBytes(12);
  const cipher = createCipheriv(ALGO, key, iv);
  const ciphertext = Buffer.concat([cipher.update(plaintext, "utf8"), cipher.final()]);
  const authTag = cipher.getAuthTag();
  return Buffer.concat([iv, authTag, ciphertext]).toString("base64");
}

export function decryptSecret(encoded: string): string {
  const key = ensureSecretKey(KEY_NAME);
  const raw = Buffer.from(encoded, "base64");
  const iv = raw.subarray(0, 12);
  const authTag = raw.subarray(12, 28);
  const ciphertext = raw.subarray(28);
  const decipher = createDecipheriv(ALGO, key, iv);
  decipher.setAuthTag(authTag);
  return Buffer.concat([decipher.update(ciphertext), decipher.final()]).toString("utf8");
}
