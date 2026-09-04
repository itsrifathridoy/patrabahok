"use client";

import { use, useState, useEffect } from "react";
import { useMailContext } from "../../../MailContext";
import { useFolderContext } from "../../../FolderContext";
import SafeHtml from "../../../SafeHtml";
import type { MessageDetail } from "../../../types";

export default function MessagePage({ params }: { params: Promise<{ messageId: string }> }) {
  const { messageId } = use(params);
  const { openCompose } = useMailContext();
  const { moveMessage, toggleFlag, markSeen } = useFolderContext();

  const [message, setMessage] = useState<MessageDetail | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setMessage(null);
    fetch(`/api/messages/${messageId}`)
      .then((res) => (res.ok ? res.json() : null))
      .then((data) => {
        if (!cancelled && data) {
          setMessage(data.message);
          markSeen(messageId);
        }
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [messageId]);

  if (loading) {
    return (
      <div className="flex-1 flex items-center justify-center text-sm" style={{ color: "var(--text-muted)" }}>
        Loading…
      </div>
    );
  }
  if (!message) {
    return (
      <div className="flex-1 flex items-center justify-center text-sm" style={{ color: "var(--text-muted)" }}>
        This message couldn&apos;t be found.
      </div>
    );
  }

  return (
    <>
      <div className="px-6 py-4 flex items-start justify-between gap-4" style={{ borderBottom: "1px solid var(--border)" }}>
        <div className="min-w-0">
          <div className="text-lg font-bold truncate">{message.subject || "(no subject)"}</div>
          <div className="text-sm mt-1" style={{ color: "var(--text-muted)" }}>
            <span className="font-medium" style={{ color: "var(--text)" }}>
              {message.fromName || message.fromAddress}
            </span>{" "}
            &lt;{message.fromAddress}&gt;
          </div>
          <div className="text-xs mt-0.5" style={{ color: "var(--text-muted)" }}>
            to {message.toAddresses?.map((a) => a.address).join(", ")}
            {" · "}
            {message.date ? new Date(message.date).toLocaleString() : ""}
          </div>
        </div>
        <div className="flex items-center gap-1 shrink-0">
          <button
            title="Star"
            onClick={() => {
              toggleFlag(message, "flagged");
              setMessage((m) => (m ? { ...m, flagged: !m.flagged } : m));
            }}
            className="w-8 h-8 rounded-lg flex items-center justify-center transition-colors"
            style={{ background: "var(--surface-2)", color: message.flagged ? "var(--accent)" : "var(--text-muted)" }}
          >
            ★
          </button>
          <button
            title="Archive"
            onClick={() => moveMessage(message.id, "archive")}
            className="w-8 h-8 rounded-lg flex items-center justify-center transition-colors"
            style={{ background: "var(--surface-2)", color: "var(--text-muted)" }}
          >
            ▤
          </button>
          <button
            title="Delete"
            onClick={() => moveMessage(message.id, "trash")}
            className="w-8 h-8 rounded-lg flex items-center justify-center transition-colors"
            style={{ background: "var(--surface-2)", color: "var(--text-muted)" }}
          >
            🗑
          </button>
        </div>
      </div>

      <div className="flex-1 overflow-y-auto scrollbar-thin px-6 py-4">
        {message.bodyHtml ? (
          <SafeHtml html={message.bodyHtml} />
        ) : (
          <pre className="text-sm whitespace-pre-wrap" style={{ fontFamily: "inherit" }}>
            {message.bodyText}
          </pre>
        )}

        {message.attachments && message.attachments.length > 0 && (
          <div className="mt-4 flex flex-wrap gap-2">
            {message.attachments.map((a) => (
              <div
                key={a.partId}
                className="text-xs rounded-lg px-3 py-2"
                style={{ background: "var(--surface-2)", border: "1px solid var(--border)" }}
              >
                📎 {a.filename} · {(a.sizeBytes / 1024).toFixed(0)} KB
              </div>
            ))}
          </div>
        )}
      </div>

      <div className="px-6 py-3 flex items-center gap-2" style={{ borderTop: "1px solid var(--border)" }}>
        <button
          onClick={() =>
            openCompose({
              to: message.fromAddress ?? "",
              subject: message.subject?.startsWith("Re:") ? message.subject : `Re: ${message.subject ?? ""}`,
              inReplyTo: message.messageIdHeader ?? undefined,
              references: message.messageIdHeader ?? undefined,
            })
          }
          className="rounded-lg px-4 py-2 text-sm font-semibold"
          style={{ background: "var(--accent)", color: "var(--accent-contrast)" }}
        >
          Reply
        </button>
        <button
          onClick={() =>
            openCompose({
              subject: message.subject?.startsWith("Fwd:") ? message.subject : `Fwd: ${message.subject ?? ""}`,
              text: `\n\n---------- Forwarded message ----------\nFrom: ${message.fromAddress}\nSubject: ${message.subject}\n\n${message.bodyText ?? ""}`,
            })
          }
          className="rounded-lg px-4 py-2 text-sm font-semibold"
          style={{ background: "var(--surface-2)", color: "var(--text)" }}
        >
          Forward
        </button>
      </div>
    </>
  );
}
