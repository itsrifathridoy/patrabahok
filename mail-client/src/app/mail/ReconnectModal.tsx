"use client";

import { useState, FormEvent } from "react";

export default function ReconnectModal({
  email,
  onClose,
  onReconnected,
}: {
  email: string;
  onClose: () => void;
  onReconnected: () => void;
}) {
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setLoading(true);
    try {
      const res = await fetch("/api/auth/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, password }),
      });
      const data = await res.json();
      if (!res.ok) {
        setError(data.error || "Could not reconnect.");
        return;
      }
      onReconnected();
    } catch {
      setError("Could not reach the server.");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-6" style={{ background: "rgba(36,28,21,0.25)" }} onClick={onClose}>
      <div
        onClick={(e) => e.stopPropagation()}
        className="w-full max-w-sm rounded-2xl p-6"
        style={{ background: "var(--surface)", border: "1px solid var(--border)", boxShadow: "var(--shadow)" }}
      >
        <div className="text-sm font-semibold mb-1">Reconnect {email}</div>
        <p className="text-xs mb-4" style={{ color: "var(--text-muted)" }}>
          The stored password no longer works — this usually means it was changed. Enter the current password to keep syncing.
        </p>
        <form onSubmit={onSubmit} className="space-y-3">
          <input
            type="password"
            required
            autoFocus
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder="Current password"
            className="w-full rounded-lg px-3 py-2 text-sm outline-none"
            style={{ background: "var(--surface)", border: "1px solid var(--border)" }}
          />
          {error && (
            <div className="text-sm rounded-lg px-3 py-2" style={{ background: "#fbeceb", color: "var(--danger)" }}>
              {error}
            </div>
          )}
          <div className="flex items-center gap-2 pt-1">
            <button
              type="submit"
              disabled={loading}
              className="rounded-lg px-4 py-2 text-sm font-semibold disabled:opacity-60"
              style={{ background: "var(--accent)", color: "var(--accent-contrast)" }}
            >
              {loading ? "Reconnecting…" : "Reconnect"}
            </button>
            <button type="button" onClick={onClose} className="text-sm" style={{ color: "var(--text-muted)" }}>
              Cancel
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
