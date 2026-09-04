export interface AccountSummary {
  id: string;
  email: string;
  displayName: string | null;
  colorTag: string;
  lastSyncedAt: string | null;
  lastSyncError: string | null;
}

export interface FolderRow {
  id: string;
  accountId: string;
  name: string;
  displayName: string;
  role: string | null;
  unreadCount: number;
  totalCount: number;
}

export interface MessageSummary {
  id: string;
  uid: number;
  subject: string | null;
  fromName: string | null;
  fromAddress: string | null;
  date: string | null;
  snippet: string | null;
  seen: boolean;
  flagged: boolean;
  hasAttachments: boolean;
}

export interface Attachment {
  filename: string;
  sizeBytes: number;
  contentType: string;
  partId: string;
}

export interface MessageDetail extends MessageSummary {
  toAddresses: { name: string; address: string }[] | null;
  ccAddresses: { name: string; address: string }[] | null;
  bodyText: string | null;
  bodyHtml: string | null;
  attachments: Attachment[] | null;
  messageIdHeader: string | null;
}
