# Admin web dashboard

A browser-based dashboard for domain/mailbox/alias management, DKIM/DNS records, and the mail
queue — a custom-built, server-rendered alternative to PostfixAdmin, not that project itself.
Webmail (reading/composing/sending mail in-browser) is a separate, much larger undertaking and
is not part of this — see [ROADMAP.md](ROADMAP.md).

## Accessing it

```
https://<your-mail-hostname>:8443/
```

Reuses the mail server's own Let's Encrypt certificate (the one issued for the hostname during
install) — no separate certificate or subdomain needed. The certificate is re-read automatically
whenever certbot renews it; no restart required.

## First login

The installer creates one admin account automatically and prints its credentials **once**,
during the `95-cli` phase — they are not recoverable afterward:

```
[ OK ]   URL:      https://mail.example.com:8443/
[ OK ]   Username: admin
[ OK ]   Password: <random>
```

If you missed it, either check `/var/log/patrabahok/install.log` (mode 600, root-only — this is
the one place the password is written to disk, and only there), or create a fresh account from
the command line:

```
patrabahok webadmin add <username>
```

Change the password from **Settings** after logging in, or manage additional accounts (for
other admins) from the **Admins** page or `patrabahok webadmin add/list/remove`.

## What it does

- **Domains** — add/remove domains
- **Mailboxes** — add/remove mailboxes, reset passwords, set quota at creation
- **Aliases** — add/remove forwarding rules
- **DKIM & DNS** — view the exact DNS records each domain needs
- **Mail queue** — view and flush Postfix's queue
- **Admins** — manage who can log into the dashboard
- **Settings** — change your own password

It's a thin UI over the same `cli/internal/mailbox` logic the CLI and JSON API use — every
action here is equally available via `patrabahok` or `patrabahokd`'s `/v1/*` endpoints.

## How it's built

Server-rendered Go (`html/template`, context-aware auto-escaping) with [htmx](https://htmx.org)
for partial-page updates and [Alpine.js](https://alpinejs.dev) for small bits of client-side
interactivity (e.g. toggling a password-reset form). No Node.js build step, no npm dependency
tree, no external CDN at runtime — both libraries are vendored under `cli/web/static/` and
compiled directly into the `patrabahokd` binary via Go's `embed` package, so the deployed server
has no separate files to keep in sync with the binary.

## Security

- **Separate login system from the API.** Dashboard accounts (`admin_users`/`admin_sessions`
  tables) use real username/password authentication with argon2id password hashing — a proper,
  slow KDF, appropriate for human-chosen secrets (unlike API tokens, which are already
  high-entropy random values and use a fast hash instead — see [SECURITY.md](SECURITY.md)).
- **Session cookies**: `HttpOnly`, `Secure`, `SameSite=Strict` — the last of these is the primary
  CSRF defense (the cookie is simply never sent on a cross-site request), which is why the
  dashboard doesn't additionally implement per-form CSRF tokens.
- **fail2ban jail**: failed logins are logged to the system's authpriv syslog facility (landing
  in `/var/log/auth.log` the same way SSH/Postfix/Dovecot auth failures do) and watched by a
  dedicated `patrabahok-dashboard` jail — same progressive-ban policy as everything else.
- **Username enumeration resistance**: a login attempt against a nonexistent username still runs
  a full password-hash verification (against a dummy hash) before responding, so response timing
  doesn't reveal whether the username exists.
- Constant-time comparison for the final hash check (`crypto/subtle`).
