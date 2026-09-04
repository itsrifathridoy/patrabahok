# Roadmap

The first release intentionally scopes down to a real, production-usable single-OS core. These
are the deferred pieces, roughly in the order they're likely to land.

## Done: multi-OS support, live-verified
Ubuntu 24.04, Ubuntu 22.04, and Debian 12 all have a full clean install pass end to end
(including the loopback delivery test and the `patrabahokd` API) on real servers. Live testing
found and fixed two more real bugs beyond what 24.04-only testing had already caught:
- Some minimal cloud images (seen on Debian 12) don't ship `rsyslog`, so `/var/log/auth.log`
  and `/var/log/mail.log` never get created — fail2ban's sshd jail hard-fails its entire startup
  without the former. `rsyslog` is now installed explicitly, and both log files are guaranteed
  to exist before fail2ban starts (rsyslog creates them lazily on first matching event, which
  may not have happened yet on a quiet fresh server).
- The distro's own `golang-go` package version varies wildly and is often too old for this
  module's Go 1.22 requirement (seen: Go 1.18 on Ubuntu 22.04, an old version on Debian 12 too).
  Fixed by no longer depending on it at all — `95-cli` now downloads a pinned Go 1.22.2 toolchain
  directly from go.dev into an ephemeral directory, SHA-256 checksum-verified the same way
  `install.sh` verifies its own release, and removes it after the build.

rspamd's own package was present and current enough in all three targets' default repos, so the
Rspamd-APT-repo fallback in `10-packages.sh` has not actually been exercised yet — it remains
unverified insurance for a target where that might not hold.

## Done: Go CLI + local API daemon, live-verified
`cli/` is a Go module: `patrabahok` (CLI) and `patrabahokd` (a systemd-managed local API daemon
on a Unix socket, token-authenticated with per-scope permissions) both link the same
`cli/internal/mailbox` business logic against the database with true parameterized queries. See
[CLI.md](CLI.md) for the full command/endpoint reference. Live-tested across all three OS
targets: every CLI command, and the API exercised over its real socket (401 without a token, 403
on wrong scope, correct data on the right scope, token list/revoke).

One real gap remains: **built from source at install time, not distributed as a prebuilt
binary.** Even with a pinned/checksummed Go toolchain, this deviates from the original plan of
cross-compiled, checksummed, pre-built release binaries (the same trust model as `install.sh`
itself) — that's a real follow-up, not just a nice-to-have, since build-from-source means every
install re-runs `go build` rather than installing a binary that went through the same
signed-release path as everything else.

## Done: custom admin web dashboard, live-verified
Built instead of installing PostfixAdmin: a server-rendered dashboard (Go `html/template` +
htmx + Alpine.js, vendored/embedded — no Node build step, no npm dependency tree, no CDN at
runtime) served directly by `patrabahokd` over HTTPS on `:8443`, reusing the mail server's own
Let's Encrypt certificate (auto-reloaded on renewal, no restart needed). Own username/password
login system (argon2id, `HttpOnly`/`Secure`/`SameSite=Strict` sessions), separate from the API's
bearer tokens, with a dedicated fail2ban jail for failed logins. See [WEB-UI.md](WEB-UI.md).
Live-tested end to end on all three OSes: login (including wrong-password rejection and the
fail2ban filter matching a real failed attempt), every page, mailbox create/delete via the
dashboard cross-checked against the CLI, password change, and session revocation on logout.

## Done: patrabahok Mail (webmail), live-verified — server deployment still pending
A real IMAP/SMTP webmail client (`mail-client/`, Next.js 15 + Postgres/Drizzle), not an
extension of the admin dashboard — a separate app, deliberately scoped: only mailboxes hosted on
this server (no OAuth, no generic multi-provider support), its own end-user login (mailbox
email+password, separate from `admin_users`), no AI features. Fast/cached by design: Postgres
caches folder/message envelopes (synced on login, every 15s, and on tab focus), full MIME bodies
fetch and cache lazily on first open rather than during sync. Multi-account: one browser profile
holds several connected mailboxes and switches between them via Gmail-style URLs
(`/mail/<accountIndex>/<folder>/<messageId>`) that always derive fresh IDs from the currently
loaded account's data — refreshing restores exactly what was open. Untrusted email HTML renders
in a sandboxed, script-disabled iframe. Mailbox passwords are AES-256-GCM encrypted before
reaching Postgres (key in `/etc/patrabahok/secrets.env`, never the database). Also required (and
got, along the way) a real mail-server-side fix unrelated to the client itself: freshly created
mailboxes only ever had INBOX, and nothing was routing spam anywhere — a global Dovecot sieve
rule now files Rspamd-flagged mail into Junk automatically for every mailbox on the server, not
just ones the webmail client happens to touch.

Live-tested end to end against the real server: real IMAP login, folder auto-creation, sync
(including a real bug found and fixed — an IMAP range-normalization quirk that reported phantom
"new" messages on every poll), lazy body fetch/parse, sending a real reply through Postfix with
delivery and a correct Sent-folder copy confirmed, star/archive/move, multi-account switching,
session survival across a restart, and the spam-to-Junk sieve rule.

**Remaining, not yet done:** deployed and tested by hand on the primary server only (Ubuntu
24.04) — not yet folded into `bin/patrabahok-installer` as a phase, not yet deployed to the
Ubuntu 22.04/Debian 12 targets, no attachment download, no draft-saving, no search, no keyboard
shortcuts, no IMAP IDLE (push — sync is timer-based, not instant).

## Done: mandatory release signing (minisign)
`install.sh` now verifies both a SHA-256 checksum (corruption/mismatched mirrors) and a minisign
signature against an embedded public key (a compromised release-publishing process itself) —
hard-fails on either check failing. The signing keypair was generated offline; the private half
exists only outside this repository and outside any CI system, never generated by or accessible
to GitHub Actions. `scripts/build-release.sh` builds a release tarball from a clean `git archive`
of HEAD; `scripts/sign-release.sh` signs it by hand with the offline key — the actual release
process, not automated. Where the OS's own package repo doesn't carry the `minisign` CLI (Ubuntu
22.04, as of this writing), `install.sh` falls back to a checksum-pinned upstream binary rather
than failing outright. See [SECURITY.md](SECURITY.md) for the full trust model.

Live-tested: the fallback binary path on Ubuntu 22.04 (apt genuinely lacks the package there —
confirmed, not assumed) and the apt-package path on Debian 12, both against a real signed release
tarball — checksum and signature both verify correctly, and a single-byte tamper to the tarball
correctly fails signature verification on both.

## Per-mailbox quota enforcement
The database already stores a `quota_bytes` value per mailbox (set via `patrabahok mailbox add
--quota`), but Dovecot currently enforces one global default quota for everyone. Wiring up
Dovecot's `dict` quota backend against the same MariaDB table would make per-mailbox quotas
actually take effect.

## MTA-STS policy hosting
The installer prints the `_mta-sts` DNS TXT record but doesn't stand up the HTTPS-hosted policy
file it requires. Since there's no longer a general-purpose web server in this stack (the admin
dashboard is a purpose-built Go binary, not Nginx), closing this would mean either a minimal
static file route added to `patrabahokd` itself, or a small standalone listener — a decision to
make when this is actually tackled, not a given.

## VM-based integration test matrix
Real mail delivery testing needs real listening ports and believable DNS in a way containers
complicate. Plan: self-hosted CI runner(s) with Vagrant + libvirt across the supported OS
matrix, a local fake DNS zone for the installer's own preflight checks, and SWAKS-driven
smoke tests asserting DKIM/DMARC/spam-score headers land correctly end-to-end.

## Multi-server / HA
Secondary MX, MariaDB replication, shared/replicated Maildir storage (dsync or a shared
filesystem), HAProxy or DNS round-robin. The schema is already multi-tenant/virtual-hosting
from day one, which this would build on rather than requiring a rework.
