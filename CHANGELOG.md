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

### Fixed
- `lib/core/migrate.sh`: a new schema migration (like `002_api_tokens.sql` above) never
  reached an already-installed server on upgrade, because phase 30-database is marked
  done and gets skipped on re-run. Pending migrations now apply unconditionally on every
  installer invocation, tracked per-file via the `schema_migrations` table.
- Some minimal cloud images (seen on Debian 12) don't ship `rsyslog`, so
  `/var/log/auth.log`/`/var/log/mail.log` never get created and fail2ban's sshd jail
  hard-failed its entire startup. `rsyslog` is now installed explicitly and both log
  files are guaranteed to exist before fail2ban starts.
- The distro's own `golang-go` package is often far too old (Go 1.18 on Ubuntu 22.04) for
  this module's Go 1.22 requirement — this is what the pinned-toolchain download above
  actually fixes, not just a nice-to-have.

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
