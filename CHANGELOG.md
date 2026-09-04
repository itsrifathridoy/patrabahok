All notable changes to this project are documented here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added
- Multi-OS support: Ubuntu 22.04 LTS and Debian 12 ("bookworm") alongside Ubuntu 24.04.
  Package installation now installs rspamd separately from the rest of the stack and
  falls back to the official Rspamd APT repository if a target's default repos don't
  provide it. The rspamd system user/group are now detected at runtime instead of
  assumed, since a third-party repo package could name them differently.
  Ubuntu 22.04 and Debian 12 are implemented but not yet live-verified on real
  servers the way Ubuntu 24.04 was (see `docs/ROADMAP.md`).
- Go CLI/API: `cli/` is now a Go module. `patrabahok` (CLI) and `patrabahokd` (a
  systemd-managed local API daemon on a Unix socket, bearer-token authenticated with
  per-scope permissions) both link the same business logic against the database with
  true parameterized queries, replacing the Bash CLI. Built from source at install
  time (temporary `golang-go` toolchain, purged after a static `CGO_ENABLED=0` build)
  rather than distributed as a prebuilt release binary — see `docs/ROADMAP.md`.
  New `api_tokens` table (schema migration `002_api_tokens.sql`).

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
