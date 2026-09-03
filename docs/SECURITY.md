# Security

## The `curl | sh` trust model — current state

`install.sh` (served at `https://patrabahok.com/install.sh`) is intentionally small (~150
lines) and does exactly this, in order:

1. Refuses to proceed on piped/non-interactive input unless `--yes` is explicitly given.
2. Resolves a release version (latest by default, or a pinned `--version vX.Y.Z`).
3. Downloads that release's tarball **and its `.sha256` checksum file** from GitHub Releases.
4. Verifies the checksum and **hard-fails, installing nothing, on any mismatch**.
5. Extracts the verified tarball under `/opt/patrabahok/releases/<version>/` and hands off to
   `bin/patrabahok-installer` inside it.

**What this protects against:** corrupted downloads, and a compromised CDN/mirror silently
serving different bytes than what's on GitHub Releases (as long as the checksum file itself
comes from the same GitHub Releases source, this at least ties the two together).

**What this does NOT yet protect against:** someone who compromises the GitHub repository or
the release-publishing process itself (they could publish a malicious tarball and a matching
checksum for it). Closing that gap requires a real code-signing step — a keypair whose private
half never touches CI, so a compromised Actions pipeline or `GITHUB_TOKEN` can't produce a
validly-signed malicious release. That (minisign-based) signing step is **planned but not yet
implemented** — see [ROADMAP.md](ROADMAP.md). Until then, treat this the way you'd treat any
`curl | sh` installer from a small project: reasonable for a self-managed server, read the
bootstrap script yourself if you want to verify it before running it as root.

## What runs as root, and why

The installer and the `patrabahok` CLI both require root — they manage system packages, write
to `/etc/postfix`, `/etc/dovecot`, `/etc/rspamd`, firewall rules, and system users (`vmail`).
There's no attempt to drop privileges mid-install; this matches how Postfix/Dovecot/MariaDB
installation and configuration normally works on Debian/Ubuntu.

## Secrets

All generated credentials (MariaDB passwords) are created with `openssl rand` and written once
to `/etc/patrabahok/secrets.env` (mode 600, root-owned). They are never echoed to the terminal,
logged, or committed to any config template in plaintext form beyond the rendered runtime config
files themselves (which are also permission-restricted — e.g. Postfix's SQL map files are
`root:postfix 640`, Dovecot's SQL config is `root:dovecot 640`).

## Network exposure

- ufw defaults to deny-incoming; only SSH, 25/587/465/993, and 80/443 (for ACME HTTP-01) are
  opened.
- The Rspamd milter socket and the Postfix↔Dovecot LMTP/auth sockets are local Unix sockets
  under `/run` and `/var/spool/postfix/private`, not exposed on any network interface.
- Dovecot only listens on IMAPS (993) — no plaintext IMAP/POP3 is exposed.
- fail2ban bans repeated auth failures against SSH, Postfix (SASL), and Dovecot, with
  progressively increasing ban times.

## TLS

TLS 1.2+ only, Mozilla "Intermediate"-equivalent cipher policy, on both Postfix (opportunistic
on port 25 for interop, mandatory on submission/smtps) and Dovecot (mandatory on IMAPS).

## DNS

DNSBL and other resolver-dependent checks go through a local `unbound` recursive resolver
(`127.0.0.1`), not your VPS provider's default forwarder — this keeps Spamhaus DNSBL usage
within their free-tier fair-use policy and reduces lookup latency.

## Reporting a vulnerability

Open a private security advisory on the GitHub repository (`itsrifathridoy/patrabahok`) rather
than a public issue.
