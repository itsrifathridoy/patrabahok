export function initials(nameOrEmail: string): string {
  const base = nameOrEmail.split("@")[0];
  const parts = base.split(/[.\s_-]+/).filter(Boolean);
  const chars = parts.length >= 2 ? parts[0][0] + parts[1][0] : base.slice(0, 2);
  return chars.toUpperCase();
}

export function formatDate(iso: string | null): string {
  if (!iso) return "";
  const d = new Date(iso);
  const now = new Date();
  const sameDay = d.toDateString() === now.toDateString();
  if (sameDay) return d.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
  const sameYear = d.getFullYear() === now.getFullYear();
  return d.toLocaleDateString([], { month: "short", day: "numeric", year: sameYear ? undefined : "numeric" });
}

export function timeAgo(iso: string): string {
  const seconds = Math.max(0, Math.floor((Date.now() - new Date(iso).getTime()) / 1000));
  if (seconds < 10) return "just now";
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

export const ROLE_LABEL: Record<string, string> = {
  inbox: "Inbox",
  sent: "Sent",
  drafts: "Drafts",
  archive: "Archive",
  trash: "Trash",
  junk: "Spam",
};

export const ROLE_ICON: Record<string, string> = {
  inbox: "◧",
  sent: "↗",
  drafts: "◫",
  archive: "▤",
  trash: "🗑",
  junk: "⚑",
};

const ACCOUNT_COLORS: Record<string, string> = {
  coral: "var(--accent)",
  mint: "var(--mint)",
  lavender: "var(--lavender)",
};

export function accountColor(colorTag: string): string {
  return ACCOUNT_COLORS[colorTag] ?? "var(--accent)";
}

export const FOLDER_ORDER: Record<string, number> = {
  inbox: 0,
  sent: 1,
  drafts: 2,
  archive: 3,
  junk: 4,
  trash: 5,
};
