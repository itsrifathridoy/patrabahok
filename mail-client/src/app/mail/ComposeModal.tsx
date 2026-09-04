"use client";

import { useState, FormEvent } from "react";

export interface ComposeDraft {
  to?: string;
  cc?: string;
  subject?: string;
  text?: string;
  inReplyTo?: string;
  references?: string;
}

export default function ComposeModal({
  accountId,
  initial,
  onClose,
  onSent,
}: {
  accountId: string;
  initial: ComposeDraft;
  onClose: () => void;
  onSent: () => void;
}) {
  const [to, setTo] = useState(initial.to ?? "");
  const [cc, setCc] = useState(initial.cc ?? "");
  const [showCc, setShowCc] = useState(Boolean(initial.cc));
  const [subject, setSubject] = useState(initial.subject ?? "");
  const [text, setText] = useState(initial.text ?? "");
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setSending(true);
    try {
      const res = await fetch(`/api/accounts/${accountId}/send`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          to,
          cc,
          subject,
          text,
          inReplyTo: initial.inReplyTo,
          references: initial.references,
        }),
      });
      const data = await res.json();
      if (!res.ok) {
        setError(data.error || "Failed to send.");
        return;
      }
      onSent();
    } catch {
      setError("Could not reach the server.");
    } finally {
      setSending(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-end justify-end p-6" style={{ background: "rgba(36,28,21,0.25)" }} onClick={onClose}>
      <div
        onClick={(e) => e.stopPropagation()}
        className="w-full max-w-lg rounded-2xl overflow-hidden flex flex-col"
        style={{ background: "var(--surface)", border: "1px solid var(--border)", boxShadow: "var(--shadow)", maxHeight: "85vh" }}
      >
        <div className="flex items-center justify-between px-4 py-3" style={{ borderBottom: "1px solid var(--border)" }}>
          <span className="text-sm font-semibold">New message</span>
          <button onClick={onClose} className="text-sm" style={{ color: "var(--text-muted)" }}>
            ✕
          </button>
        </div>

        <form onSubmit={onSubmit} className="flex flex-col overflow-y-auto">
          <div className="px-4 py-2 flex items-center gap-2" style={{ borderBottom: "1px solid var(--border)" }}>
            <span className="text-xs w-10" style={{ color: "var(--text-muted)" }}>
              To
            </span>
            <input
              value={to}
              onChange={(e) => setTo(e.target.value)}
              placeholder="name@example.com, another@example.com"
              className="flex-1 text-sm outline-none bg-transparent py-1"
              required
            />
            {!showCc && (
              <button type="button" onClick={() => setShowCc(true)} className="text-xs" style={{ color: "var(--text-muted)" }}>
                Cc
              </button>
            )}
          </div>
          {showCc && (
            <div className="px-4 py-2 flex items-center gap-2" style={{ borderBottom: "1px solid var(--border)" }}>
              <span className="text-xs w-10" style={{ color: "var(--text-muted)" }}>
                Cc
              </span>
              <input value={cc} onChange={(e) => setCc(e.target.value)} className="flex-1 text-sm outline-none bg-transparent py-1" />
            </div>
          )}
          <div className="px-4 py-2" style={{ borderBottom: "1px solid var(--border)" }}>
            <input
              value={subject}
              onChange={(e) => setSubject(e.target.value)}
              placeholder="Subject"
              className="w-full text-sm font-medium outline-none bg-transparent py-1"
            />
          </div>
          <div className="px-4 py-3 flex-1">
            <textarea
              value={text}
              onChange={(e) => setText(e.target.value)}
              placeholder="Write your message…"
              rows={10}
              className="w-full text-sm outline-none bg-transparent resize-none"
              required
            />
          </div>

          {error && (
            <div className="mx-4 mb-2 text-sm rounded-lg px-3 py-2" style={{ background: "#fbeceb", color: "var(--danger)" }}>
              {error}
            </div>
          )}

          <div className="px-4 py-3 flex items-center justify-between" style={{ borderTop: "1px solid var(--border)" }}>
            <button
              type="submit"
              disabled={sending}
              className="rounded-lg px-5 py-2 text-sm font-semibold disabled:opacity-60"
              style={{ background: "var(--accent)", color: "var(--accent-contrast)" }}
            >
              {sending ? "Sending…" : "Send"}
            </button>
            <button type="button" onClick={onClose} className="text-sm" style={{ color: "var(--text-muted)" }}>
              Discard
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
