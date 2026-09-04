import { NextRequest, NextResponse } from "next/server";
import { and, eq } from "drizzle-orm";
import { db } from "@/db";
import { folders } from "@/db/schema";
import { sendMail } from "@/lib/smtp";
import { appendMessage, syncFolder } from "@/lib/imap";
import { requireAccount, UnauthorizedError, NotFoundError } from "@/lib/require-account";

function splitAddresses(value: unknown): string[] {
  if (typeof value !== "string") return [];
  return value
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);
}

export async function POST(req: NextRequest, { params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  try {
    const account = await requireAccount(id);
    const body = await req.json().catch(() => null);

    const to = splitAddresses(body?.to);
    const cc = splitAddresses(body?.cc);
    const bcc = splitAddresses(body?.bcc);
    const subject = typeof body?.subject === "string" ? body.subject : "";
    const text = typeof body?.text === "string" ? body.text : "";
    if (to.length === 0) return NextResponse.json({ error: "At least one recipient is required." }, { status: 400 });
    if (!text.trim()) return NextResponse.json({ error: "Message body cannot be empty." }, { status: 400 });

    const raw = await sendMail(account, {
      to,
      cc: cc.length ? cc : undefined,
      bcc: bcc.length ? bcc : undefined,
      subject,
      text,
      inReplyTo: typeof body?.inReplyTo === "string" ? body.inReplyTo : undefined,
      references: typeof body?.references === "string" ? body.references : undefined,
    });

    const [sentFolder] = await db
      .select()
      .from(folders)
      .where(and(eq(folders.accountId, account.id), eq(folders.role, "sent")))
      .limit(1);
    if (sentFolder) {
      await appendMessage(account, sentFolder.name, raw);
      // Pull the newly appended copy into the cache immediately so Sent looks right
      // without the admin having to wait for the next periodic sync.
      await syncFolder(account, sentFolder);
    }

    return NextResponse.json({ ok: true });
  } catch (err) {
    if (err instanceof UnauthorizedError) return NextResponse.json({ error: "not logged in" }, { status: 401 });
    if (err instanceof NotFoundError) return NextResponse.json({ error: "not found" }, { status: 404 });
    return NextResponse.json({ error: (err as Error).message || "Failed to send." }, { status: 502 });
  }
}
