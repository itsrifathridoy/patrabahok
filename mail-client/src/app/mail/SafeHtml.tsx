"use client";

import { useRef, useState, useEffect } from "react";

/**
 * Renders untrusted email HTML inside a sandboxed iframe. This is the one part of a
 * webmail client where getting it wrong is a real vulnerability: email HTML is
 * attacker-controlled by definition (anyone who can send you mail can send you HTML),
 * so it must never be rendered via dangerouslySetInnerHTML in the main document — that
 * would let an email execute arbitrary script in the context of a logged-in session.
 *
 * `sandbox="allow-same-origin"` with no `allow-scripts` means the iframe can render CSS
 * and layout normally but any <script>, onclick=, or javascript: URL in the message is
 * inert. `referrerPolicy="no-referrer"` avoids leaking the fact a message was read (and
 * from where) via remote image requests.
 */
export default function SafeHtml({ html }: { html: string }) {
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const [height, setHeight] = useState(200);

  useEffect(() => {
    const iframe = iframeRef.current;
    if (!iframe) return;
    const resize = () => {
      const doc = iframe.contentDocument;
      if (doc?.documentElement) {
        setHeight(Math.min(doc.documentElement.scrollHeight + 24, 3000));
      }
    };
    iframe.addEventListener("load", resize);
    return () => iframe.removeEventListener("load", resize);
  }, [html]);

  const doc = `<!doctype html><html><head><meta charset="utf-8">
    <base target="_blank">
    <style>
      body { margin: 0; padding: 0; font-family: -apple-system, "Segoe UI", sans-serif; font-size: 14px; color: #241c15; word-wrap: break-word; }
      img { max-width: 100%; height: auto; }
      a { color: #ff6a3d; }
    </style>
  </head><body>${html}</body></html>`;

  return (
    <iframe
      ref={iframeRef}
      srcDoc={doc}
      sandbox="allow-same-origin allow-popups"
      referrerPolicy="no-referrer"
      style={{ width: "100%", height, border: "none" }}
      title="Message content"
    />
  );
}
