import {
  pgTable,
  uuid,
  text,
  timestamp,
  integer,
  boolean,
  jsonb,
  uniqueIndex,
  index,
} from "drizzle-orm/pg-core";

// A "profile" is a browser/device-level login, not tied to any single mailbox — it can
// hold several connected accounts (mailboxes), which is what lets someone log in once
// and switch between multiple mailboxes without re-authenticating each time.
export const profiles = pgTable("profiles", {
  id: uuid("id").primaryKey().defaultRandom(),
  createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
});

export const sessions = pgTable("sessions", {
  id: uuid("id").primaryKey().defaultRandom(),
  profileId: uuid("profile_id")
    .notNull()
    .references(() => profiles.id, { onDelete: "cascade" }),
  tokenHash: text("token_hash").notNull(),
  createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
  expiresAt: timestamp("expires_at", { withTimezone: true }).notNull(),
}, (t) => ({
  tokenHashIdx: uniqueIndex("sessions_token_hash_idx").on(t.tokenHash),
}));

// An IMAP/SMTP-connected mailbox. passwordEncrypted is AES-256-GCM ciphertext (see
// src/lib/crypto.ts) — never the plaintext password — encrypted with a key that lives
// outside the database entirely (in /etc/patrabahok/secrets.env, root-only), the same
// pattern the admin dashboard already uses for its own stored Cloudflare credentials.
export const accounts = pgTable("accounts", {
  id: uuid("id").primaryKey().defaultRandom(),
  profileId: uuid("profile_id")
    .notNull()
    .references(() => profiles.id, { onDelete: "cascade" }),
  email: text("email").notNull(),
  displayName: text("display_name"),
  imapHost: text("imap_host").notNull(),
  imapPort: integer("imap_port").notNull().default(993),
  smtpHost: text("smtp_host").notNull(),
  smtpPort: integer("smtp_port").notNull().default(587),
  username: text("username").notNull(),
  passwordEncrypted: text("password_encrypted").notNull(),
  colorTag: text("color_tag").notNull().default("coral"),
  lastSyncedAt: timestamp("last_synced_at", { withTimezone: true }),
  lastSyncError: text("last_sync_error"),
  createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
}, (t) => ({
  profileEmailIdx: uniqueIndex("accounts_profile_email_idx").on(t.profileId, t.email),
}));

export const folders = pgTable("folders", {
  id: uuid("id").primaryKey().defaultRandom(),
  accountId: uuid("account_id")
    .notNull()
    .references(() => accounts.id, { onDelete: "cascade" }),
  name: text("name").notNull(), // raw IMAP mailbox path, e.g. "INBOX", "Sent"
  displayName: text("display_name").notNull(),
  role: text("role"), // 'inbox' | 'sent' | 'drafts' | 'trash' | 'archive' | null
  uidValidity: text("uid_validity"), // stored as text: IMAP UIDVALIDITY can exceed int32
  lastSeenUid: integer("last_seen_uid").notNull().default(0),
  unreadCount: integer("unread_count").notNull().default(0),
  totalCount: integer("total_count").notNull().default(0),
}, (t) => ({
  accountNameIdx: uniqueIndex("folders_account_name_idx").on(t.accountId, t.name),
}));

export const messages = pgTable("messages", {
  id: uuid("id").primaryKey().defaultRandom(),
  accountId: uuid("account_id")
    .notNull()
    .references(() => accounts.id, { onDelete: "cascade" }),
  folderId: uuid("folder_id")
    .notNull()
    .references(() => folders.id, { onDelete: "cascade" }),
  uid: integer("uid").notNull(),
  messageIdHeader: text("message_id_header"),
  inReplyTo: text("in_reply_to"),
  subject: text("subject"),
  fromName: text("from_name"),
  fromAddress: text("from_address"),
  toAddresses: jsonb("to_addresses").$type<{ name: string; address: string }[]>(),
  ccAddresses: jsonb("cc_addresses").$type<{ name: string; address: string }[]>(),
  date: timestamp("date", { withTimezone: true }),
  snippet: text("snippet"),
  bodyText: text("body_text"),
  bodyHtml: text("body_html"),
  bodyFetched: boolean("body_fetched").notNull().default(false),
  seen: boolean("seen").notNull().default(false),
  flagged: boolean("flagged").notNull().default(false),
  hasAttachments: boolean("has_attachments").notNull().default(false),
  attachments: jsonb("attachments").$type<
    { filename: string; sizeBytes: number; contentType: string; partId: string }[]
  >(),
  sizeBytes: integer("size_bytes"),
  createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
}, (t) => ({
  folderUidIdx: uniqueIndex("messages_folder_uid_idx").on(t.folderId, t.uid),
  accountDateIdx: index("messages_account_date_idx").on(t.accountId, t.date),
}));
