"use client";

import { createContext, useContext } from "react";
import type { FolderRow, MessageSummary } from "./types";

export interface FolderContextValue {
  folder: FolderRow | null;
  messages: MessageSummary[];
  loading: boolean;
  refreshMessages: () => Promise<void>;
  toggleFlag: (msg: MessageSummary, flag: "seen" | "flagged") => Promise<void>;
  moveMessage: (messageId: string, toRole: "archive" | "trash") => Promise<void>;
  markSeen: (messageId: string) => void;
}

export const FolderContext = createContext<FolderContextValue | null>(null);

export function useFolderContext(): FolderContextValue {
  const ctx = useContext(FolderContext);
  if (!ctx) throw new Error("useFolderContext must be used within the mail folder layout");
  return ctx;
}
