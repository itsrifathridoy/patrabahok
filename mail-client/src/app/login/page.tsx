"use client";

import { useState, FormEvent } from "react";
import { useRouter } from "next/navigation";

export default function LoginPage() {
  const router = useRouter();
  const [email, setEmail] = useState("");
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
        setError(data.error || "Could not sign in.");
        return;
      }
      router.push("/mail");
      router.refresh();
    } catch {
      setError("Could not reach the server. Try again.");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center px-4" style={{ background: "var(--bg)" }}>
      <div
        className="w-full max-w-sm rounded-2xl p-8"
        style={{ background: "var(--surface)", border: "1px solid var(--border)", boxShadow: "var(--shadow)" }}
      >
        <div className="mb-6">
          <div className="text-lg font-bold tracking-tight">patrabahok Mail</div>
          <p className="text-sm mt-1" style={{ color: "var(--text-muted)" }}>
            Sign in with your mailbox address and password.
          </p>
        </div>

        <form onSubmit={onSubmit} className="space-y-4">
          <div>
            <label className="block text-xs font-semibold mb-1" style={{ color: "var(--text-muted)" }}>
              Email address
            </label>
            <input
              type="email"
              required
              autoFocus
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="you@yourdomain.com"
              className="w-full rounded-lg px-3 py-2 text-sm outline-none"
              style={{ background: "var(--surface)", border: "1px solid var(--border)" }}
            />
          </div>
          <div>
            <label className="block text-xs font-semibold mb-1" style={{ color: "var(--text-muted)" }}>
              Password
            </label>
            <input
              type="password"
              required
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              className="w-full rounded-lg px-3 py-2 text-sm outline-none"
              style={{ background: "var(--surface)", border: "1px solid var(--border)" }}
            />
          </div>

          {error && (
            <div className="text-sm rounded-lg px-3 py-2" style={{ background: "#fbeceb", color: "var(--danger)" }}>
              {error}
            </div>
          )}

          <button
            type="submit"
            disabled={loading}
            className="w-full rounded-lg py-2.5 text-sm font-semibold transition-colors disabled:opacity-60"
            style={{ background: "var(--accent)", color: "var(--accent-contrast)" }}
          >
            {loading ? "Signing in…" : "Sign in"}
          </button>
        </form>

        <p className="text-xs mt-6" style={{ color: "var(--text-muted)" }}>
          Use the same email and password as your mailbox&apos;s IMAP/webmail login. This
          server never sees or needs any other account.
        </p>
      </div>
    </div>
  );
}
