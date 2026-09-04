# Roadmap

The first release intentionally scopes down to a real, production-usable single-OS core. These
are the deferred pieces, roughly in the order they're likely to land.

## Multi-OS support — implemented, pending live verification
Ubuntu 22.04 LTS and Debian 12 ("bookworm") are now handled by `lib/core/os.sh` and
`lib/phases/10-packages.sh` (rspamd is installed separately with a fallback to the official
Rspamd APT repo if a target's default repos lack it; the rspamd system user/group are detected
at runtime rather than assumed). This has **not yet been live-tested** on real Ubuntu 22.04 or
Debian 12 servers the way Ubuntu 24.04 was — that live-testing pass (the same process that found
and fixed 8 real bugs on 24.04) is still needed before calling these two targets verified.

## Go CLI + local API daemon
Replace/extend the Bash `patrabahok` CLI with a Go binary (`patrabahok`) plus a `patrabahokd`
daemon exposing a local Unix-socket JSON API (token-authenticated, scoped permissions). Gives
true parameterized DB queries (closing the SQL-injection-by-construction risk the Bash CLI
mitigates only via input validation/escaping), real unit tests, and a stable integration point
for future tooling (e.g. a future web UI, automation, monitoring hooks). Distributed the same
way as the installer itself: cross-compiled static binaries, checksummed, released alongside
each tag.

## PostfixAdmin
Optional web UI (PHP + Nginx + PHP-FPM) for managing domains/mailboxes/aliases against the same
MariaDB schema, for admins who prefer a browser over the CLI.

## Roundcube
Optional webmail client (PHP + Nginx + PHP-FPM), IMAP/SMTP backend pointing at the local
Dovecot/Postfix.

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
file it requires. A minimal Nginx vhost serving `/.well-known/mta-sts.txt` (reusing the web
server that PostfixAdmin/Roundcube would also need) would close this gap.

## VM-based integration test matrix
Real mail delivery testing needs real listening ports and believable DNS in a way containers
complicate. Plan: self-hosted CI runner(s) with Vagrant + libvirt across the supported OS
matrix, a local fake DNS zone for the installer's own preflight checks, and SWAKS-driven
smoke tests asserting DKIM/DMARC/spam-score headers land correctly end-to-end.

## Multi-server / HA
Secondary MX, MariaDB replication, shared/replicated Maildir storage (dsync or a shared
filesystem), HAProxy or DNS round-robin. The schema is already multi-tenant/virtual-hosting
from day one, which this would build on rather than requiring a rework.
