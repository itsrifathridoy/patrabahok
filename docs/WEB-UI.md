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

- **Overview** — live counts (domains/mailboxes/aliases/tokens), which services are up, mail
  queue state, and disk usage, all in one glance
- **Domains** — add/remove domains; adding one shows a "Configure DNS & verify →" prompt straight
  into DNS Analysis for that domain, plus a one-click "Auto-configure via Cloudflare" button right
  there if a matching zone is found. Also where you connect Cloudflare in the first place — see
  below.
- **Mailboxes** — add/remove mailboxes, reset passwords, set quota at creation
- **Aliases** — add/remove forwarding rules
- **DNS analysis** — the step-by-step DNS records a domain needs (A/MX/SPF/DKIM/DMARC), plus a
  **live verify**: it flushes the local resolver's cache for those exact names first (so a check
  right after fixing DNS doesn't report a stale negative result) and then queries the server's own
  resolver, comparing what's actually published against what this server expects — including
  comparing the live DKIM TXT record against the key this server currently signs with, so a stale
  record (e.g. after a reinstall regenerated the key) shows up as a clear "fail" instead of
  silently breaking delivery. If the domain's zone is on a connected Cloudflare account, an
  **"Auto-configure DNS via Cloudflare"** button creates/updates all the required records directly
  via the Cloudflare API instead of copying them by hand.
- **Mail queue** — view and flush Postfix's queue
- **Diagnostics** — service status, `postfix check`/`doveconf -n`/`rspamadm configtest` output,
  TLS certificate expiry, disk usage, recent mail-log errors/warnings/bounces, and current
  fail2ban bans — everything you'd otherwise SSH in and check by hand
- **API tokens** — create/revoke `patrabahokd` bearer tokens and set their scopes, without
  touching the CLI
- **Admins** — manage who can log into the dashboard
- **Settings** — change your own password

It's a thin UI over the same `cli/internal/mailbox`/`authtoken`/`diag`/`dnscheck` logic the CLI
and JSON API use — every action here is equally available via `patrabahok` or `patrabahokd`'s
`/v1/*` endpoints, so the dashboard has full CLI feature parity.

## How it's built

Server-rendered Go (`html/template`, context-aware auto-escaping) with [htmx](https://htmx.org)
for partial-page updates and [Alpine.js](https://alpinejs.dev) for small bits of client-side
interactivity (e.g. toggling a password-reset form). No Node.js build step, no npm dependency
tree, no external CDN at runtime — both libraries are vendored under `cli/web/static/` and
compiled directly into the `patrabahokd` binary via Go's `embed` package, so the deployed server
has no separate files to keep in sync with the binary.

## Cloudflare integration

Connect an account from the **Domains** page (not Settings — the connect flow lives right where
you add domains, so it's there the moment you need it, and a one-click "Auto-configure via
Cloudflare" button appears in the post-add confirmation whenever the newly added domain's zone is
found in the connected account) to auto-configure DNS on the DNS Analysis page instead of copying
records by hand. Two ways to connect:

- **OAuth (recommended)** — register an OAuth client under your own Cloudflare account (Manage
  Account → OAuth clients → Create client, Authorization Code grant, redirect URL = the one shown
  on the Domains page, scoped to DNS Write + Zone Read), paste its Client ID/Secret, then click **Connect
  with Cloudflare** to complete Cloudflare's own consent screen. Access tokens are refreshed
  automatically using the stored refresh token.
- **API token** — simpler, no redirect URL to register: create a scoped token at Cloudflare → My
  Profile → API Tokens → Create Token (`Zone:DNS:Edit`, `Zone:Zone:Read`) and paste it directly.

Either way, only an AES-GCM-encrypted value ever reaches the database — the encryption key lives
in `/etc/patrabahok/secrets.env` (root-only, 0600), generated on first use the same way the
installer's own secrets are (`cli/internal/secretkey`), so a database-only compromise doesn't hand
over live Cloudflare API access.

Auto-configure is conservative: it only ever creates or updates the exact records a domain needs
(A on the mail hostname, MX, SPF/DMARC/DKIM TXT at their specific names) and never touches
unrelated records at the same name (e.g. another TXT record for a different service sitting at the
domain apex, or other MX records already pointing elsewhere).

## Security

- **Separate login system from the API.** Dashboard accounts (`admin_users`/`admin_sessions`
  tables) use real username/password authentication with argon2id password hashing — a proper,
  slow KDF, appropriate for human-chosen secrets (unlike API tokens, which are already
  high-entropy random values and use a fast hash instead — see [SECURITY.md](SECURITY.md)).
- **Session cookies**: `HttpOnly`, `Secure`, `SameSite=Lax`. Lax (not Strict) is required so the
  session survives the redirect back from Cloudflare's OAuth consent screen — a cross-site
  top-level navigation, which Strict cookies are never sent on — while still blocking the cookie
  on cross-site POST/fetch requests, which is what actually matters for CSRF on state-changing
  actions. The OAuth flow itself additionally uses its own short-lived, random `state` cookie,
  the standard OAuth CSRF defense, independent of the session cookie's policy.
- **fail2ban jail**: failed logins are logged to the system's authpriv syslog facility (landing
  in `/var/log/auth.log` the same way SSH/Postfix/Dovecot auth failures do) and watched by a
  dedicated `patrabahok-dashboard` jail — same progressive-ban policy as everything else.
- **Username enumeration resistance**: a login attempt against a nonexistent username still runs
  a full password-hash verification (against a dummy hash) before responding, so response timing
  doesn't reveal whether the username exists.
- Constant-time comparison for the final hash check (`crypto/subtle`).
