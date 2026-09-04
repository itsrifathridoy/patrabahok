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

## Webmail
Reading/composing/sending mail in-browser — not built, and explicitly out of scope for now: an
order of magnitude larger than the admin dashboard (IMAP sync, MIME parsing, XSS-safe HTML
rendering of arbitrary email content, attachments, compose/send, search). A future project in
its own right rather than an extension of the current dashboard.

## Mandatory release signing (minisign)
Currently `install.sh` verifies a SHA-256 checksum of the release tarball, which protects
against corruption/mismatched mirrors but not a compromise of the release-publishing process
itself. The plan: generate a minisign keypair offline, embed the public key in `install.sh`,
and require every release to carry a valid signature from a private key that never touches CI —
so a compromised GitHub Actions pipeline or token can't produce a validly-signed malicious
release. See [SECURITY.md](SECURITY.md) for the current state.

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
