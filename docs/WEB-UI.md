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

The navigation follows how an organization thinks about its mail, not how the server is built:

- **Overview** — counts of users (and how many are suspended), groups, domains and total mail
  storage, service health, quick actions, users per domain, disk and queue status, and recent
  activity from the audit log
- **Directory**
  - **Users** — searchable, filterable list (by domain and status) with each user's storage use,
    paginated for large schools. "Add user" generates a strong password on request. Each user has
    a profile page with:
    - name, storage use and limit
    - password reset
    - suspend and restore (blocks sign-in and delivery but keeps their mail)
    - email forwarding, with or without keeping a copy
    - alternate addresses (aliases)
    - group memberships
    - their recent audit history
    - deletion
  - **Import users** — create up to 1,000 users from a CSV (`email,name,password,quota`). Missing
    passwords are generated, and the passwords are offered once as a downloadable credentials
    file. Each row succeeds or fails on its own.
  - **Groups** — distribution lists (see below)
  - **Aliases** — extra addresses for a mailbox, filterable by domain
- **Organization**
  - **Domains** — every domain with its user, group and alias counts. Each domain page has its
    public API key, catch-all address, connection settings to give users, Cloudflare one-click
    DNS, and deletion (you have to type the domain name to confirm).
  - **DNS & deliverability** — the records a domain needs (A/MX/SPF/DKIM/DMARC) and a **live
    verify**. It flushes the local resolver's cache for those names first, so a check right after
    fixing DNS isn't stale. It then compares what's published with what this server expects,
    including the DKIM key it actually signs with, and offers Cloudflare auto-configure, DKIM
    rotation and MTA-STS hosting.
  - **Mail queue** — view and flush Postfix's queue
- **Security**
  - **Administrators** — who can sign in to the dashboard
  - **Audit log** — every change from the dashboard, the CLI, local API tokens and domain API
    keys: who made it, when, what it targeted, and the client IP. It's searchable, and passwords
    are never recorded.
  - **API access** — local API tokens, plus pointers to the per-domain public API keys
- **System**
  - **Diagnostics** — service status, `postfix check`/`doveconf -n`/`rspamadm configtest` output,
    TLS expiry, disk use, recent mail-log problems, and current fail2ban bans
  - **Settings** — change your own password

The top bar has a global search across users, groups and domains (press <kbd>/</kbd>), which
jumps straight to an exact address match. There's also a light/dark toggle (it follows the system
by default) and the account menu. Every change shows a confirmation toast, destructive actions ask
first in a styled dialog, and the layout collapses to a drawer menu on phones.

### Groups

A group is one address, say `class10@school.edu`, that delivers to every member. This is the
Google Groups/Workspace model:

- **Members** can be users, aliases, other groups (groups nest, so `everyone@` can contain
  `teachers@` and `students@`), and, if the group allows it, external addresses such as parents'
  Gmail accounts. Paste many addresses at once. Each one is validated on its own, and problems
  are listed without losing the rest.
- **Roles**: member or owner.
- **Who can post**: anyone, anyone in the organization (signed in), members only, or owners only
  (announcement lists). This is checked by `patrabahokd`'s Postfix policy service when the
  message arrives, so a sender who isn't allowed gets an immediate SMTP error instead of a silent
  drop. See [ARCHITECTURE.md](ARCHITECTURE.md#groups-forwarding-and-catch-all).
- Suspended members are skipped automatically. An empty group rejects mail until it has a member.

It's a thin UI over the same `cli/internal/mailbox`/`authtoken`/`diag`/`dnscheck` logic the CLI
and JSON APIs use, and every action is also available from `patrabahok` and the APIs.

## How it's built

Server-rendered Go (`html/template`, context-aware auto-escaping) with [htmx](https://htmx.org)
for partial-page updates and [Alpine.js](https://alpinejs.dev) for small bits of client-side
interactivity (e.g. toggling a password-reset form). No Node.js build step, no npm dependency
tree, no external CDN at runtime — both libraries are vendored under `cli/web/static/` and
compiled directly into the `patrabahokd` binary via Go's `embed` package, so the deployed server
has no separate files to keep in sync with the binary.

The visual design is a small hand-written design system in `cli/web/static/style.css`: color,
radius and shadow tokens, light and dark palettes, and components such as cards, stat cards,
tables, badges, modals (native `<dialog>`), dropdowns, toasts and empty states. Its fonts are
self-hosted (Plus Jakarta Sans and JetBrains Mono, SIL OFL, `static/fonts/`), and its icons are
inline SVG templates (`templates/icons.html`, Lucide-style). Shared template partials are in
`templates/components.html`. `static/app.js` holds the few behaviors every page shares: theme,
mobile navigation, toasts, modals, styled confirmations, `/` to search, and the password
generator.

Handlers follow one pattern for changes: do it, record it in the audit log, then redirect with a
flash message, which shows as a toast on the next page. Failures return a plain-text message that
shows as an error toast.

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
- **Cross-origin protection**: state-changing requests carrying a foreign `Origin` header are
  rejected (in addition to the `SameSite` cookie), and every response sends
  `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, `Referrer-Policy` and HSTS.
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
