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
| CLI/API/UI | Go (`cli/` module — `patrabahok` CLI, `patrabahokd` local API + admin web dashboard) | One codebase, true parameterized DB queries, real type-checked code. The dashboard is server-rendered (`html/template` + htmx + Alpine.js, no build toolchain/CDN) rather than a PHP app like PostfixAdmin. Built from source at install time (not yet distributed as a prebuilt release binary — see ROADMAP.md) |

## Why Rspamd instead of Amavis/SpamAssassin/OpenDKIM/OpenDMARC

Every actively-developed self-hosted mail project (mailcow, docker-mailserver, Mailu) has moved
to or defaults to Rspamd. It natively does spam scoring, DKIM signing *and* verification, DMARC
policy checking, and ARC — collapsing four separate daemons/milters into one, wired into Postfix
as a single milter socket (`smtpd_milters`/`non_smtpd_milters`). This removes an entire layer of
inter-process glue (Amavis↔SpamAssassin↔ClamAV↔OpenDKIM↔OpenDMARC) that is a common source of
fragility in hand-rolled mail server installers.

**The milter socket is TCP loopback (`127.0.0.1:11332`), not a Unix socket.** Real cross-server
testing found a reproducible case (Ubuntu 22.04) where Postfix's milter `connect()` failed with
ENOENT against a Unix socket that demonstrably existed with correct ownership/permissions — it
survived a socket-path change, a directory-permission fix, and a full clean Postfix restart, and
a raw `strace` confirmed the kernel itself returning ENOENT for that exact path from Postfix's
worker process, while a manual connection as the same user from a plain script succeeded. Root
cause not conclusively identified. Switching to TCP loopback resolved it immediately and has
been reliable across all three OSes since — it also sidesteps the entire class of
filesystem/chroot/permission ambiguity a Unix socket has to get right in the first place.

**Rspamd's `use_esld` must be `false` for this project.** Its default (`true`) signs outbound
mail using the *effective second-level domain* — a message from `sales.example.com` would look
for `example.com`'s DKIM key. patrabahok generates and stores one key per exact domain string a
user adds (`patrabahok domain add`), with no assumption that a subdomain shares its parent
domain's key, so key lookup has to match the exact configured domain, not Rspamd's ESLD-collapsed
default. This surfaced in testing as DKIM signing silently not happening for a subdomain-style
test domain, with no error anywhere except a single log line in Rspamd's own log file.

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

## Groups, forwarding and catch-all

All three are plain Postfix virtual alias expansion driven from MariaDB. One query in
`templates/postfix/mysql-virtual-alias-maps.cf.tmpl` returns, for an address:

1. its plain alias destinations (including an `@domain` catch-all)
2. if it's a group, its members, minus suspended mailboxes
3. if it's a mailbox with forwarding, the forwarding target
4. the mailbox itself, unless it forwards without keeping a copy

Mapping every mailbox to itself (part 4) is what makes a catch-all safe. Without it, Postfix's
`@domain` lookup would also capture mail for addresses that exist. Postfix re-expands every
result, so a group whose member is another group just works (nesting).
`virtual_alias_expansion_limit` is raised to 20000 so a school-wide group isn't truncated.

**Posting policies are enforced at SMTP time** by a Postfix policy delegation service inside
`patrabahokd` (`cli/internal/policy`, `127.0.0.1:10040`). It runs first in
`smtpd_recipient_restrictions` on port 25 and on submission/smtps (via the
`patrabahok_submission_recipient_restrictions` parameter, because master.cf `-o` values can't
contain spaces), so it also applies to signed-in users. For each recipient that is a group it
decides based on:

- the authenticated SASL username if there is one, otherwise the envelope sender
- an unauthenticated sender in a *hosted* domain is never trusted (anyone can claim it over port
  25), so local users must sign in to post to a restricted group
- unauthenticated loopback clients (local scripts) are trusted, like `permit_mynetworks`; signed-in sessions are always checked, even over loopback, since webmail submits through 127.0.0.1:587
- `+tag` addresses are matched to the group, as Postfix itself does

Anything that isn't a group gets `DUNNO`. If the service is unreachable, Postfix temp-fails
(`smtpd_policy_service_default_action = 451`) rather than letting restricted posts through.
Phase `55-mail-routing` applies all of this on upgrades, and makes sure `patrabahokd` is new
enough to serve the policy service before Postfix starts relying on it.

## Public HTTPS listener (:443)

`patrabahokd` owns `:443` directly (`cli/internal/frontdoor`). It doesn't sit behind a separate
web server, and it routes each request by host and path:

```
https://mta-sts.<domain>/.well-known/mta-sts.txt  ──▶ MTA-STS policy (internal/mtasts)
https://<hostname>/api/<domain>/...               ──▶ public per-domain API (internal/publicapi)
https://<hostname>/<anything else>                ──▶ reverse proxy to the webmail on loopback:
                                                        Roundcube: nginx + PHP-FPM, 127.0.0.1:8081
                                                        patrabahok Mail: Next.js, 127.0.0.1:3000
```

- TLS certificates are picked per connection by SNI. `mta-sts.*` names get their own
  per-domain certificate, and every other name gets the mail hostname's certificate. Both reload
  on renewal without a restart.
- The public API's first path segment is a domain name, so it always contains a dot.
  patrabahok Mail's own `/api/accounts`, `/api/auth`, ... routes have no dot and still go to the
  webmail. That's how both share the `/api/` prefix.
- The webmail backends never see the internet directly. The proxy keeps the original `Host` and
  replaces any client-supplied `X-Forwarded-*` headers, so the client IP the backend logs (and
  fail2ban bans) can't be spoofed.
- Nothing listens on `:80`, because certbot's standalone HTTP-01 issuance and renewal need it.
  That's why Roundcube's nginx binds loopback only and the distro's default site is removed.
- Roundcube is a pinned, checksum-verified upstream release (`/opt/roundcube/releases/<version>`,
  `current` symlink) rather than the distro package. The distro package's version differs across
  the three supported OSes, and it pulls in Apache and interactive dbconfig-common prompts.

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

- Webmail is now an installer phase (`90-webmail`: Roundcube or patrabahok Mail). Roundcube's
  install was tested in a Debian 12 container (schema, nginx/PHP-FPM, login page, password-plugin
  grants), but neither option has had a full live install with a real login on the three target
  OSes yet
- The Go CLI/API/dashboard is live-verified on all three supported OSes, but is built from
  source at install time (pinned/checksummed Go toolchain) rather than distributed as a
  prebuilt, checksummed release binary
