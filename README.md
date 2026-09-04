# patrabahok

A production-oriented, self-hosted mail server installer for Ubuntu 24.04/22.04 LTS and
Debian 12. One command
sets up Postfix, Dovecot, MariaDB (virtual multi-domain mailboxes), Rspamd + ClamAV (spam
scoring, DKIM signing/verification, DMARC, antivirus), Let's Encrypt TLS, ufw + fail2ban, and
a local recursive resolver — plus a `patrabahok` CLI and a local `patrabahokd` API for day-2
domain/mailbox management.

```
curl -sSL https://patrabahok.com/install.sh | sh -s -- --yes
```

Read `install.sh` before you pipe it into a root shell — see [docs/SECURITY.md](docs/SECURITY.md)
for exactly what the bootstrap does and how it verifies what it downloads.

## What you get

- Postfix (MTA) + Dovecot (IMAP/LMTP), virtual multi-domain mailboxes backed by MariaDB
- Maildir storage under a dedicated, shell-less `vmail` user
- Rspamd: spam scoring, DKIM signing + verification, DMARC checking, ARC, ClamAV antivirus —
  as a single milter (see [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for why, vs. the more
  common Amavis/SpamAssassin/OpenDKIM/OpenDMARC combination)
- Let's Encrypt TLS via certbot, auto-renewing, TLS 1.2+ only
- postscreen + Spamhaus DNSBL greylisting at the connection stage, via a local `unbound`
  recursive resolver (not your provider's default DNS, for Spamhaus fair-use compliance)
- ufw (default-deny) + fail2ban (sshd, postfix, dovecot jails, progressive ban times)
- `patrabahok` CLI + `patrabahokd` local API (Unix socket, token-scoped): `domain`, `mailbox`,
  `alias`, `dkim`, `dns`, `queue`, `status`

## Requirements

- A fresh Ubuntu 24.04, Ubuntu 22.04, or Debian 12 server, run as root — all three are
  live-tested end to end (see [docs/ROADMAP.md](docs/ROADMAP.md))
- A domain you control, with the ability to add DNS records
- Outbound port 25 not blocked by your VPS provider (the installer checks and warns)

## Quick start

```
curl -sSL https://patrabahok.com/install.sh | sh -s -- --yes
```

The installer will ask for your domain, mail hostname, and admin email; check DNS; install
and configure everything; issue a TLS certificate; and print the exact DNS records (MX, SPF,
DKIM, DMARC) you need to add. See [docs/INSTALL.md](docs/INSTALL.md) for the full walkthrough,
non-interactive/scripted install, and how to add more domains and mailboxes afterward.

## Documentation

- [docs/INSTALL.md](docs/INSTALL.md) — full install walkthrough and options
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — design and component choices
- [docs/CLI.md](docs/CLI.md) — `patrabahok` command reference
- [docs/SECURITY.md](docs/SECURITY.md) — threat model and the curl\|sh trust model
- [docs/DNS-RECORDS.md](docs/DNS-RECORDS.md) — every DNS record explained
- [docs/BACKUP-RESTORE.md](docs/BACKUP-RESTORE.md) — backup and disaster recovery
- [docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md) — common failure signatures
- [docs/ROADMAP.md](docs/ROADMAP.md) — what's not built yet (PostfixAdmin, Roundcube,
  a prebuilt/checksummed CLI release binary, mandatory release signing, VM-based CI)

## Status

This is an early release, but not an untested one. It's built to be genuinely
production-usable for a single-server, single-to-multi-domain mail setup — Ubuntu 24.04,
Ubuntu 22.04, and Debian 12 have each had a full clean install live-tested end to end
(services, the `patrabahokd` API, and a real send→milter→LMTP→Maildir delivery test), and
Ubuntu 24.04 additionally verified with a real Gmail delivery (SPF/DKIM/DMARC all passing).
Review
[docs/ROADMAP.md](docs/ROADMAP.md) for everything else intentionally out of scope so far.

## License

MIT — see [LICENSE](LICENSE).
