"use client";

import { use, useState, useEffect, useCallback } from "react";
import Link from "next/link";
import { useSelectedLayoutSegment } from "next/navigation";
import { useMailContext } from "../../MailContext";
import { FolderContext, type FolderContextValue } from "../../FolderContext";
import type { MessageSummary } from "../../types";
import { formatDate, ROLE_LABEL } from "../../utils";

export default function FolderLayout({
  children,
  params,
}: {
  children: React.ReactNode;
  params: Promise<{ accountIndex: string; folder: string }>;
}) {
  const { accountIndex, folder: folderRole } = use(params);
  const { account, folders } = useMailContext();
  const folder = folders.find((f) => (f.role ?? "") === folderRole) ?? null;
  const activeMessageId = useSelectedLayoutSegment(); // the [messageId] segment, or null

  const [messages, setMessages] = useState<MessageSummary[]>([]);
  const [loading, setLoading] = useState(false);

  const refreshMessages = useCallback(async () => {
    if (!account || !folder) return;
    setLoading(true);
    try {
      const res = await fetch(`/api/accounts/${account.id}/folders/${folder.id}/messages`);
      if (!res.ok) return;
      const data = await res.json();
      setMessages(data.messages);
    } finally {
      setLoading(false);
    }
  }, [account, folder]);

  // Re-fetch on folder switch AND whenever background sync changes this folder's
  // counts (see [accountIndex]/layout.tsx's periodic sync) — one effect covers both.
  useEffect(() => {
    refreshMessages();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [account?.id, folder?.id, folder?.totalCount, folder?.unreadCount]);

  const toggleFlag = useCallback(async (msg: MessageSummary, flag: "seen" | "flagged") => {
    const value = flag === "seen" ? !msg.seen : !msg.flagged;
    setMessages((prev) => prev.map((m) => (m.id === msg.id ? { ...m, [flag]: value } : m)));
    await fetch(`/api/messages/${msg.id}/flag`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ flag, value }),
    });
  }, []);

  const moveMessage = useCallback(async (messageId: string, toRole: "archive" | "trash") => {
    await fetch(`/api/messages/${messageId}/move`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ toRole }),
    });
    setMessages((prev) => prev.filter((m) => m.id !== messageId));
  }, []);

  const markSeen = useCallback((messageId: string) => {
    setMessages((prev) => prev.map((m) => (m.id === messageId ? { ...m, seen: true } : m)));
  }, []);

  const ctxValue: FolderContextValue = { folder, messages, loading, refreshMessages, toggleFlag, moveMessage, markSeen };

  return (
    <>
      <section className="w-96 shrink-0 flex flex-col" style={{ borderRight: "1px solid var(--border)" }}>
        <div className="px-5 py-4" style={{ borderBottom: "1px solid var(--border)" }}>
          <div className="text-xl font-bold tracking-tight">{ROLE_LABEL[folderRole] ?? folder?.displayName ?? " "}</div>
          <div className="text-xs mt-0.5" style={{ color: "var(--text-muted)" }}>
            {loading ? "Syncing…" : `${messages.length} messages`}
          </div>
        </div>
        <div className="flex-1 overflow-y-auto scrollbar-thin">
          {messages.length === 0 && !loading && (
            <div className="text-sm text-center py-10" style={{ color: "var(--text-muted)" }}>
              No messages here.
            </div>
          )}
          {messages.map((m) => (
            <Link
              key={m.id}
              href={`/mail/${accountIndex}/${folderRole}/${m.id}`}
              className="w-full text-left px-5 py-3.5 block transition-colors"
              style={{
                background: m.id === activeMessageId ? "var(--surface-2)" : "transparent",
                borderBottom: "1px solid var(--border)",
                borderLeft: m.id === activeMessageId ? "3px solid var(--accent)" : "3px solid transparent",
              }}
            >
              <div className="flex items-center justify-between gap-2">
                <span className="text-sm truncate" style={{ fontWeight: m.seen ? 500 : 700 }}>
                  {m.fromName || m.fromAddress || "(unknown sender)"}
                </span>
                <span className="text-xs shrink-0" style={{ color: "var(--text-muted)" }}>
                  {formatDate(m.date)}
                </span>
              </div>
              <div className="text-sm truncate mt-0.5" style={{ fontWeight: m.seen ? 400 : 600 }}>
                {m.subject || "(no subject)"}
              </div>
              {m.snippet && (
                <div className="text-xs truncate mt-0.5" style={{ color: "var(--text-muted)" }}>
                  {m.snippet}
                </div>
              )}
              <div className="flex items-center gap-2 mt-1.5">
                {!m.seen && <span className="w-1.5 h-1.5 rounded-full" style={{ background: "var(--accent)" }} />}
                {m.flagged && <span style={{ color: "var(--accent)" }}>★</span>}
                {m.hasAttachments && <span style={{ color: "var(--text-muted)" }}>📎</span>}
              </div>
            </Link>
          ))}
        </div>
      </section>

      <FolderContext.Provider value={ctxValue}>
        <section className="flex-1 flex flex-col overflow-hidden">{children}</section>
      </FolderContext.Provider>
    </>
  );
}
