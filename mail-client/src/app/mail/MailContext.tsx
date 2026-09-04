"use client";

import { createContext, useContext } from "react";
import type { AccountSummary, FolderRow } from "./types";
import type { ComposeDraft } from "./ComposeModal";

export interface MailContextValue {
  accounts: AccountSummary[];
  accountIndex: number;
  account: AccountSummary | null;
  folders: FolderRow[];
  refreshAccounts: () => Promise<void>;
  refreshFolders: () => Promise<void>;
  openCompose: (draft: ComposeDraft) => void;
  openAddAccount: () => void;
}

export const MailContext = createContext<MailContextValue | null>(null);

export function useMailContext(): MailContextValue {
  const ctx = useContext(MailContext);
  if (!ctx) throw new Error("useMailContext must be used within the mail account layout");
  return ctx;
}
