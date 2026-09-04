All notable changes to this project are documented here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added
- Multi-OS support: Ubuntu 22.04 LTS and Debian 12 ("bookworm") alongside Ubuntu 24.04.
  Package installation now installs rspamd separately from the rest of the stack and
  falls back to the official Rspamd APT repository if a target's default repos don't
  provide it. The rspamd system user/group are now detected at runtime instead of
  assumed, since a third-party repo package could name them differently.
  Live-tested end to end on real Ubuntu 22.04 and Debian 12 servers alongside Ubuntu 24.04.
- Go CLI/API: `cli/` is now a Go module. `patrabahok` (CLI) and `patrabahokd` (a
  systemd-managed local API daemon on a Unix socket, bearer-token authenticated with
  per-scope permissions) both link the same business logic against the database with
  true parameterized queries, replacing the Bash CLI. Built from source at install
  time from a pinned, checksum-verified Go 1.22.2 toolchain (downloaded fresh, removed
  after a static `CGO_ENABLED=0` build) rather than distributed as a prebuilt release
  binary — see `docs/ROADMAP.md`. New `api_tokens` table (schema migration
  `002_api_tokens.sql`). Live-tested (every CLI command, and the API over its real
  socket) on all three supported OSes.

- Admin web dashboard: a custom server-rendered UI (Go `html/template` + htmx + Alpine.js,
  vendored/embedded, no Node/npm/CDN) served by `patrabahokd` over HTTPS on `:8443`, reusing
  the mail server's own Let's Encrypt certificate (auto-reloaded on renewal). Domains,
  mailboxes, aliases, DKIM/DNS records, mail queue, and dashboard-admin management — built
  instead of installing PostfixAdmin. Own login system separate from API tokens
  (`admin_users`/`admin_sessions`, schema migration `003_admin_web.sql`): argon2id password
  hashing, `HttpOnly`/`Secure`/`SameSite=Lax` sessions, a dedicated fail2ban jail for
  failed logins, username-enumeration-resistant timing. New `patrabahok webadmin
  add/list/remove` CLI commands; the installer creates one admin account automatically and
  prints its password once. See `docs/WEB-UI.md`. Live-tested end to end (login incl. wrong
  password, the fail2ban filter matching a real failed attempt, every page, mailbox
  create/delete cross-checked against the CLI, password change, session revocation on
  logout) on all three supported OSes.

- Dashboard: Overview home page (live counts, service status, queue/disk state), a DNS
  Analysis page with per-domain setup steps and a live "Verify DNS now" check
  (`cli/internal/dnscheck`) that queries the server's own resolver and compares actual
  A/MX/SPF/DMARC/DKIM records — including the published DKIM key vs. the key currently
  signing mail — against what's expected, a Diagnostics page (`cli/internal/diag`:
  service status, `postfix check`/`doveconf -n`/`rspamadm configtest`, TLS expiry, disk
  usage, recent mail-log issues, fail2ban bans), and an API Tokens page for full CLI
  parity. Adding a domain now links straight into DNS Analysis for it. Live-tested on
  all three supported OSes, including a rebuild via the real installer's own `95-cli`
  phase (not just a dev shortcut) followed by a full `verify` pass.

- Cloudflare integration (`cli/internal/cloudflare`, schema migrations `004`/`005`): when a
  domain's zone is on a connected Cloudflare account, DNS Analysis gets an
  "Auto-configure DNS via Cloudflare" button that creates/updates the required A/MX/SPF/
  DMARC/DKIM records directly via the Cloudflare API — idempotent and conservative (never
  touches unrelated records at a shared name). Two ways to connect from Settings: OAuth
  2.0 Authorization Code flow ("Connect with Cloudflare", using an OAuth client the admin
  registers under their own Cloudflare account) or a manually pasted scoped API token.
  Either way only an AES-GCM-encrypted value reaches the database — the key lives in
  `/etc/patrabahok/secrets.env`, generated on first use (`cli/internal/secretkey`).
  Live-tested: OAuth client save, the authorize redirect (verified against the real
  `dash.cloudflare.com/oauth2/auth` endpoint), manual token verification against the real
  Cloudflare API, and disconnect, on all three supported OSes.

### Fixed
- `lib/core/migrate.sh`: a new schema migration (like `002_api_tokens.sql` above) never
  reached an already-installed server on upgrade, because phase 30-database is marked
  done and gets skipped on re-run. Pending migrations now apply unconditionally on every
  installer invocation, tracked per-file via the `schema_migrations` table.
- Domains added after initial install (via the CLI, API, or dashboard) got no DKIM key
  and no DNS records dump — that generation only ever ran for the domain(s) known at
  install time. `mailbox.Store.DomainAdd` now provisions both, for every domain, through
  every interface, since it's the one shared path all three use.
- `patrabahokd`'s systemd sandbox (`ProtectSystem=strict`, `ProtectHome=read-only`) silently
  blocked the dashboard process from writing the DKIM key, the DNS records dump, or a
  first-use secrets-file key — even running as root, sandboxing still applies. Moved the
  DNS dump out from under `/root` (which stays blocked even via `ReadWritePaths=` in
  practice) to a dedicated `/var/lib/patrabahok/dns-records`, and added it plus
  `/var/lib/rspamd/dkim` and `/etc/patrabahok` to `ReadWritePaths=`.
- `95-cli`'s `systemctl enable --now` never restarts an already-running dashboard, so a
  re-run that rebuilds the binaries or changes the unit file (as above) silently left the
  OLD process running until something else restarted it. Now does `enable` + an explicit
  `restart`.
- DNS Analysis's live verify could report a domain's records as "not found" even after
  they were correctly published, because the local resolver had negative-cached an
  earlier "doesn't exist yet" answer (RFC 2308) from before the admin added them — a
  retry right after fixing DNS wouldn't reflect reality until that cache entry's TTL
  expired. The verify now flushes the local resolver's cache for the exact names it's
  about to check first.
- Some minimal cloud images (seen on Debian 12) don't ship `rsyslog`, so
  `/var/log/auth.log`/`/var/log/mail.log` never get created and fail2ban's sshd jail
  hard-failed its entire startup. `rsyslog` is now installed explicitly and both log
  files are guaranteed to exist before fail2ban starts.
- The distro's own `golang-go` package is often far too old (Go 1.18 on Ubuntu 22.04) for
  this module's Go 1.22 requirement — this is what the pinned-toolchain download above
  actually fixes, not just a nice-to-have.
- DKIM signing was silently not happening for some domains: Rspamd's `use_esld` default
  (`true`) signs using the effective second-level domain, so a domain like
  `u22.example.com` looked for `example.com`'s key. Since patrabahok generates one key per
  exact configured domain string with no such assumption, `use_esld` is now `false`.
- Rspamd's milter connection intermittently/reproducibly failed with ENOENT on a Unix
  socket that demonstrably existed with correct permissions (seen on Ubuntu 22.04, survived
  a socket-path change, a directory-permission fix, and a full clean Postfix restart — see
  `docs/ARCHITECTURE.md`). Switched the milter transport to TCP loopback
  (`127.0.0.1:11332`), which resolved it immediately.

## [0.1.0] - 2026-09-04

### Added
- Initial MVP: `install.sh` bootstrap with SHA-256 verified release fetch.
- Core installer (`bin/patrabahok-installer`) with phased, idempotent, resumable execution.
- Postfix + Dovecot + MariaDB virtual mailbox stack (Maildir, multi-domain).
- Rspamd + ClamAV for spam scoring, DKIM signing/verification, DMARC, and antivirus.
- Let's Encrypt TLS via certbot with auto-renewal deploy hooks.
- ufw firewall + fail2ban jails for sshd/postfix/dovecot.
- Local `unbound` recursive resolver for DNSBL/Rspamd lookups.
- `patrabahok` CLI for domain/mailbox/alias/DKIM/queue management.
- Target OS: Ubuntu 24.04 LTS only. Other OSes, PostfixAdmin, Roundcube, and the Go
  CLI/API daemon are tracked in `docs/ROADMAP.md`.
