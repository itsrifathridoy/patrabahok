import nodemailer from "nodemailer";
import MailComposer from "nodemailer/lib/mail-composer";
import { decryptSecret } from "./crypto";
import { SMTP_HOST, SMTP_PORT } from "./mailserver";
import type { accounts as AccountsTable } from "@/db/schema";

type Account = typeof AccountsTable.$inferSelect;

export interface OutgoingMessage {
  to: string[];
  cc?: string[];
  bcc?: string[];
  subject: string;
  text: string;
  html?: string;
  inReplyTo?: string;
  references?: string;
}

function buildRaw(account: Account, msg: OutgoingMessage): Promise<Buffer> {
  const composer = new MailComposer({
    from: account.displayName ? `"${account.displayName}" <${account.email}>` : account.email,
    to: msg.to.join(", "),
    cc: msg.cc?.join(", "),
    bcc: msg.bcc?.join(", "),
    subject: msg.subject,
    text: msg.text,
    html: msg.html,
    inReplyTo: msg.inReplyTo,
    references: msg.references,
  });
  return new Promise((resolve, reject) => {
    composer.compile().build((err, message) => {
      if (err) reject(err);
      else resolve(message);
    });
  });
}

/** Sends the message and returns the exact raw RFC 822 bytes that went out over SMTP —
 * the caller appends those same bytes to the account's Sent folder via IMAP, so what
 * shows up in Sent is byte-identical to what was actually delivered. */
export async function sendMail(account: Account, msg: OutgoingMessage): Promise<Buffer> {
  const raw = await buildRaw(account, msg);

  const transport = nodemailer.createTransport({
    host: SMTP_HOST,
    port: SMTP_PORT,
    secure: SMTP_PORT === 465,
    requireTLS: SMTP_PORT !== 465,
    auth: {
      user: account.email,
      pass: decryptSecret(account.passwordEncrypted),
    },
  });

  await transport.sendMail({ raw, envelope: { from: account.email, to: [...msg.to, ...(msg.cc ?? []), ...(msg.bcc ?? [])] } });
  return raw;
}
