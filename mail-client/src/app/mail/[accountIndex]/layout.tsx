"use client";

import { use, useState, useEffect, useCallback, useRef } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import type { AccountSummary, FolderRow } from "../types";
import { MailContext, type MailContextValue } from "../MailContext";
import ComposeModal, { type ComposeDraft } from "../ComposeModal";
import AddAccountModal from "../AddAccountModal";
import { initials, accountColor, timeAgo, ROLE_LABEL, ROLE_ICON } from "../utils";

const SYNC_INTERVAL_MS = 15_000;

export default function AccountLayout({
  children,
  params,
}: {
  children: React.ReactNode;
  params: Promise<{ accountIndex: string }>;
}) {
  const { accountIndex: accountIndexStr } = use(params);
  const accountIndex = Number(accountIndexStr) || 0;
  const router = useRouter();

  const [accounts, setAccounts] = useState<AccountSummary[]>([]);
  const [folders, setFolders] = useState<FolderRow[]>([]);
  const [switcherOpen, setSwitcherOpen] = useState(false);
  const [addAccountOpen, setAddAccountOpen] = useState(false);
  const [composeDraft, setComposeDraft] = useState<ComposeDraft | null>(null);
  const [composeAccountId, setComposeAccountId] = useState<string | null>(null);

  const account = accounts[accountIndex] ?? null;

  const refreshAccounts = useCallback(async () => {
    const res = await fetch("/api/accounts");
    if (res.status === 401) {
      router.push("/login");
      return;
    }
    const data = await res.json();
    setAccounts(data.accounts);
  }, [router]);

  const refreshFolders = useCallback(async () => {
    if (!account) return;
    const res = await fetch(`/api/accounts/${account.id}/folders`);
    if (!res.ok) return;
    const data = await res.json();
    setFolders(data.folders);
    // A fresh account may have folders that don't exist as a route target yet — if
    // we're sitting on a folder role with none matching (shouldn't normally happen
    // since Inbox always exists), fall back quietly rather than erroring.
  }, [account]);

  useEffect(() => {
    refreshAccounts();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    if (account) refreshFolders();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [account?.id]);

  const syncingRef = useRef(false);
  const [syncing, setSyncing] = useState(false);
  const runSync = useCallback(async () => {
    if (!account || syncingRef.current) return;
    syncingRef.current = true;
    setSyncing(true);
    try {
      await fetch(`/api/accounts/${account.id}/sync`, { method: "POST" });
      await Promise.all([refreshFolders(), refreshAccounts()]);
    } finally {
      syncingRef.current = false;
      setSyncing(false);
    }
  }, [account, refreshFolders, refreshAccounts]);

  useEffect(() => {
    if (!account) return;
    const id = setInterval(runSync, SYNC_INTERVAL_MS);
    const onVisible = () => {
      if (document.visibilityState === "visible") runSync();
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      clearInterval(id);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [account, runSync]);

  const ctxValue: MailContextValue = {
    accounts,
    accountIndex,
    account,
    folders,
    refreshAccounts,
    refreshFolders,
    openCompose: (draft) => {
      setComposeAccountId(account?.id ?? null);
      setComposeDraft(draft);
    },
    openAddAccount: () => setAddAccountOpen(true),
  };

  return (
    <div className="h-screen flex" style={{ background: "var(--bg)" }}>
      <aside className="w-64 shrink-0 flex flex-col p-4 gap-4" style={{ borderRight: "1px solid var(--border)" }}>
        <div className="relative">
          <button
            onClick={() => setSwitcherOpen((v) => !v)}
            className="w-full flex items-center gap-3 rounded-xl px-3 py-2 text-left transition-shadow hover:shadow-sm"
            style={{ background: "var(--surface)", border: "1px solid var(--border)" }}
          >
            <span
              className="w-8 h-8 rounded-full flex items-center justify-center text-xs font-bold shrink-0"
              style={{ background: account ? accountColor(account.colorTag) : "var(--accent)", color: "var(--accent-contrast)" }}
            >
              {account ? initials(account.displayName ?? account.email) : "…"}
            </span>
            <span className="flex-1 min-w-0">
              <div className="text-sm font-semibold truncate">{account?.displayName ?? account?.email ?? "Loading…"}</div>
              <div className="text-xs truncate" style={{ color: "var(--text-muted)" }}>
                {account?.email}
              </div>
            </span>
            <span style={{ color: "var(--text-muted)" }}>⌄</span>
          </button>

          {switcherOpen && (
            <div
              className="absolute left-0 right-0 mt-1 rounded-xl overflow-hidden z-20"
              style={{ background: "var(--surface)", border: "1px solid var(--border)", boxShadow: "var(--shadow)" }}
            >
              {accounts.map((acc, idx) => (
                <Link
                  key={acc.id}
                  href={`/mail/${idx}/inbox`}
                  onClick={() => setSwitcherOpen(false)}
                  className="w-full flex items-center gap-2 px-3 py-2 text-left text-sm"
                  style={{ background: idx === accountIndex ? "var(--surface-2)" : "transparent" }}
                >
                  <span
                    className="w-6 h-6 rounded-full flex items-center justify-center text-[10px] font-bold shrink-0"
                    style={{ background: accountColor(acc.colorTag), color: "var(--accent-contrast)" }}
                  >
                    {initials(acc.displayName ?? acc.email)}
                  </span>
                  <span className="truncate">{acc.email}</span>
                </Link>
              ))}
              <button
                onClick={() => {
                  setSwitcherOpen(false);
                  setAddAccountOpen(true);
                }}
                className="w-full px-3 py-2 text-left text-sm font-medium"
                style={{ color: "var(--accent)", borderTop: "1px solid var(--border)" }}
              >
                + Add account
              </button>
              <button
                onClick={async () => {
                  await fetch("/api/auth/logout", { method: "POST" });
                  router.push("/login");
                }}
                className="w-full px-3 py-2 text-left text-sm"
                style={{ color: "var(--danger)", borderTop: "1px solid var(--border)" }}
              >
                Sign out
              </button>
            </div>
          )}
        </div>

        <button
          onClick={() => {
            setComposeAccountId(account?.id ?? null);
            setComposeDraft({});
          }}
          className="rounded-xl py-2.5 text-sm font-semibold transition-transform active:scale-[0.98]"
          style={{ background: "var(--accent)", color: "var(--accent-contrast)", boxShadow: "0 4px 12px rgba(255,106,61,0.25)" }}
        >
          + Compose
        </button>

        <nav className="flex flex-col gap-0.5">
          {folders.map((f) => (
            <Link
              key={f.id}
              href={`/mail/${accountIndex}/${f.role ?? "inbox"}`}
              className="flex items-center gap-2 rounded-lg px-3 py-2 text-sm text-left transition-colors"
              style={{ color: "var(--text)" }}
            >
              <span className="w-4 text-center opacity-80">{ROLE_ICON[f.role ?? ""] ?? "▢"}</span>
              <span className="flex-1 truncate">{ROLE_LABEL[f.role ?? ""] ?? f.displayName}</span>
              {f.unreadCount > 0 && (
                <span
                  className="text-[10px] font-bold rounded-full px-1.5 py-0.5"
                  style={{ background: "var(--surface-2)", color: "var(--text-muted)" }}
                >
                  {f.unreadCount}
                </span>
              )}
            </Link>
          ))}
        </nav>

        <div className="mt-auto flex flex-col gap-2">
          <button
            onClick={runSync}
            disabled={syncing}
            className="flex items-center justify-center gap-1.5 text-xs rounded-lg px-3 py-2 transition-colors disabled:opacity-70"
            style={{ background: "var(--surface-2)", color: "var(--text-muted)" }}
          >
            <span style={{ display: "inline-block", animation: syncing ? "spin 1s linear infinite" : undefined }}>⟳</span>
            {syncing ? "Syncing…" : account?.lastSyncedAt ? `Synced ${timeAgo(account.lastSyncedAt)}` : "Not synced yet"}
          </button>
          {account?.lastSyncError && (
            <div className="text-xs rounded-lg px-3 py-2" style={{ background: "#fbeceb", color: "var(--danger)" }}>
              Sync issue: {account.lastSyncError}
            </div>
          )}
        </div>
      </aside>

      <MailContext.Provider value={ctxValue}>{children}</MailContext.Provider>

      {composeDraft && composeAccountId && (
        <ComposeModal
          accountId={composeAccountId}
          initial={composeDraft}
          onClose={() => setComposeDraft(null)}
          onSent={() => {
            setComposeDraft(null);
            refreshFolders();
          }}
        />
      )}
      {addAccountOpen && (
        <AddAccountModal
          onClose={() => setAddAccountOpen(false)}
          onAdded={async () => {
            setAddAccountOpen(false);
            await refreshAccounts();
          }}
        />
      )}
    </div>
  );
}
