// This client only ever connects to the patrabahok mail server it's deployed alongside
// (see the project's scope decision: no generic multi-provider/OAuth support) — so
// host/port are fixed deployment config, not something a user ever enters. Deliberately
// the server's public mail hostname, not 127.0.0.1/localhost: the TLS certificate is
// issued for that hostname, and connecting under any other name would fail hostname
// verification (or require disabling it, which we don't want to do).
export const IMAP_HOST = process.env.PATRABAHOK_IMAP_HOST || "localhost";
export const IMAP_PORT = Number(process.env.PATRABAHOK_IMAP_PORT || 993);
export const SMTP_HOST = process.env.PATRABAHOK_SMTP_HOST || "localhost";
export const SMTP_PORT = Number(process.env.PATRABAHOK_SMTP_PORT || 587);
