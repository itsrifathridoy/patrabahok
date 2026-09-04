# Architecture

## Component choices

| Layer | Choice | Why |
|---|---|---|
| MTA | Postfix | Modular, security-focused, well-documented milter/postscreen support |
| MDA/IMAP | Dovecot | SQL-backed virtual users, LMTP delivery, efficient IMAP |
| Storage | MariaDB (virtual domains/users/aliases) + Maildir under `vmail` | Multi-tenant from day one, no system users per mailbox |
| Spam/AV/DKIM/DMARC | **Rspamd** + ClamAV | See below — replaces the more traditional Amavis+SpamAssassin+OpenDKIM+OpenDMARC chain |
| TLS | Let's Encrypt via certbot | Free, automatable, standard renewal tooling |
| Firewall | ufw | Approachable default-deny for a single-role server; integrates with fail2ban's `ufw` action |
| DNS resolver | local `unbound` | DNSBL fair-use (Spamhaus) + latency; system-wide via `/etc/resolv.conf` |
| Intrusion prevention | fail2ban | jails for sshd, postfix, dovecot with progressive ban times |
| CLI/API | Go (`cli/` module — `patrabahok` CLI + `patrabahokd` local API daemon) | True parameterized DB queries, real type-checked code, a stable local API for automation. Built from source at install time (not yet distributed as a prebuilt release binary — see ROADMAP.md) |

## Why Rspamd instead of Amavis/SpamAssassin/OpenDKIM/OpenDMARC

Every actively-developed self-hosted mail project (mailcow, docker-mailserver, Mailu) has moved
to or defaults to Rspamd. It natively does spam scoring, DKIM signing *and* verification, DMARC
policy checking, and ARC — collapsing four separate daemons/milters into one, wired into Postfix
as a single milter socket (`smtpd_milters`/`non_smtpd_milters`). This removes an entire layer of
inter-process glue (Amavis↔SpamAssassin↔ClamAV↔OpenDKIM↔OpenDMARC) that is a common source of
fragility in hand-rolled mail server installers.

## Request flow

```
Internet ──25/587/465──▶ postscreen (DNSBL via local unbound) ──▶ Postfix smtpd
                                                                        │
                                                            smtpd_milters (Rspamd)
                                                     spam score · DKIM sign/verify
                                                        DMARC check · ClamAV scan
                                                                        │
                                                        virtual_alias/mailbox_maps
                                                              (MariaDB lookup)
                                                                        │
                                                        virtual_transport = lmtp
                                                                        ▼
                                                            Dovecot LMTP ──▶ Maildir
                                                          (SQL auth, vmail uid/gid)
                                                                        ▲
                                                          IMAPS (993) ──┘  (client access)
```

## Idempotency and state

`bin/patrabahok-installer` runs `lib/phases/NN-*.sh` in numeric order. Each phase is a
standalone `bash` subprocess (not sourced into the parent) so a failure inside one phase can't
leave the parent installer's own `set -euo pipefail` in an inconsistent state, and so cross-phase
values must be persisted explicitly — via `/etc/patrabahok/state.json` (`state_set`/`state_get`,
see `lib/core/state.sh`) for non-secret config, and `/etc/patrabahok/secrets.env` (`secret_ensure`,
see `lib/core/secrets.sh`) for generated credentials. A phase already marked `done` in
`state.json` is skipped on re-run; `--force-phase NAME` re-runs one phase regardless.

## Supply chain (`install.sh`)

See [SECURITY.md](SECURITY.md) for the full trust model. In short: the bootstrap served at
`patrabahok.com/install.sh` only ever downloads a GitHub Release tarball, verifies its SHA-256
checksum, and hands off to the verified `bin/patrabahok-installer` inside it — it never executes
unverified remote code itself.

## Known MVP limitations (see ROADMAP.md)

- No PostfixAdmin / Roundcube web UIs yet
- The Go CLI/API is live-verified on all three supported OSes, but is built from source at
  install time (pinned/checksummed Go toolchain) rather than distributed as a prebuilt,
  checksummed release binary
- Per-mailbox quota is collected but not yet enforced dynamically (a single global default
  quota is enforced via Dovecot's static quota plugin)
- No MTA-STS policy hosting (the DNS record text is printed, but you must host the policy
  file yourself if you want it)
