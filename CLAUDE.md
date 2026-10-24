# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

patrabahok is a self-hosted mail server installer for Ubuntu 24.04/22.04 and Debian 12 (run as root on a fresh server). It has three parts:

1. **Bash installer** (`install.sh`, `bin/patrabahok-installer`, `lib/`, `templates/`, `sql/`). It sets up Postfix, Dovecot, MariaDB, Rspamd + ClamAV, certbot, ufw + fail2ban and unbound.
2. **Go module** (`cli/`). It holds the `patrabahok` CLI and the `patrabahokd` daemon (JSON API + admin web dashboard + optional MTA-STS server). Phase `95-cli.sh` builds it from source at install time with a checksum-pinned Go toolchain.
3. **Webmail** (`mail-client/`, "patrabahok Mail"). A Next.js 15 + Postgres/Drizzle IMAP/SMTP client. It is one of the two webmail options in installer phase `90-webmail`; the other is Roundcube.

User-facing docs live in `docs/`. `docs/ARCHITECTURE.md` explains the design decisions, `docs/API.md` documents every `patrabahokd` endpoint, and `docs/CLI.md` is the CLI reference.

## Commands

CI (`.github/workflows/lint.yml`) runs only these two lint scripts. The repo has no unit tests.

```sh
bash tests/shellcheck.sh   # shellcheck -x --severity=warning --exclude=SC1091 over install.sh, bin/, lib/**/*.sh, scripts/*.sh
bash tests/govet.sh        # in cli/: gofmt -l (must be empty), go vet ./..., CGO_ENABLED=0 go build ./...
```

Go (from `cli/`, go 1.22; keep the installer's pinned `GO_VERSION` in `lib/phases/95-cli.sh` compatible):
```sh
go build ./cmd/patrabahok ./cmd/patrabahokd
gofmt -w .
```

Webmail (from `mail-client/`): `npm run dev`, `npm run build`, `npm run lint`, `npm run db:generate` / `npm run db:migrate` (drizzle-kit).

Installer (on a target server, as root):
```sh
bin/patrabahok-installer install [--non-interactive --config answers.env] [--force-phase 50-postfix]
bin/patrabahok-installer status | verify
```

Release: `scripts/build-release.sh` builds `dist/patrabahok-v<VERSION>.tar.gz` + `.sha256` from `git archive HEAD`, so only committed files ship. Then `scripts/sign-release.sh` signs it with minisign using the offline key. `install.sh` checks both the checksum and the signature against the `RELEASE_PUBKEY` embedded in it. `tools/build-release.sh` is an older copy-based packager; the docs point to `scripts/`.

## Installer architecture

- `bin/patrabahok-installer install` runs each `lib/phases/NN-*.sh` in numeric order, **each as its own `bash` subprocess**. Each phase script sources the `lib/core/*.sh` helpers it needs through `$PATRABAHOK_HOME` (which is why shellcheck excludes SC1091). Values a later phase needs must be written down explicitly:
  - non-secret config goes to `/etc/patrabahok/state.json` via `state_set`/`state_get` (`lib/core/state.sh`, uses jq)
  - generated credentials go to `/etc/patrabahok/secrets.env` (0600) via `secret_ensure`/`secrets_load` (`lib/core/secrets.sh`)
- A phase already marked `done` in `state.json` is skipped on re-run, so phases must be idempotent and safe to resume after a failure.
- **Templates**: `render_template TMPL OUT KEY=VAL...` (`lib/core/template.sh`) replaces only `{{KEY}}` placeholders. `$var`/`${var}` text passes through untouched because it belongs to the target program (for example Postfix macros). Leftover `{{UPPER_CASE}}` placeholders trigger a warning.
- **DB migrations**: `sql/schema/NNN_*.sql`. Each file must end with `INSERT IGNORE INTO schema_migrations (version) VALUES (N);`. `apply_pending_migrations` (`lib/core/migrate.sh`) runs on every installer invocation, not only in phase `30-database`, so upgrades pick up new migrations. Write migrations so they are safe on existing data.
- **Webmail** (`90-webmail.sh`, choice asked in `00-preflight` and stored as state `webmail` = `roundcube|custom|none`) only ever binds loopback and records `webmail_upstream` in state. `render_patrabahokd_unit` (`lib/core/patrabahokd.sh`) puts that into patrabahokd's unit. Nothing may listen on `:80`, because certbot standalone renewal needs it. Roundcube and Node.js are pinned upstream downloads with SHA-256 checksums in the phase, the same pattern as the Go toolchain in `95-cli.sh`.
- `install.sh` is a deliberately small **POSIX sh** bootstrap (no bashisms). It downloads, verifies, extracts to `/opt/patrabahok/releases/<ver>`, and execs the real installer. Any change to its trust logic is security-sensitive (see `docs/SECURITY.md`).

## Go architecture (`cli/`)

- Business logic lives in `internal/mailbox` (domains, mailboxes, aliases, DKIM provisioning/rotation) and uses parameterized MariaDB queries. The CLI calls it **directly against the DB** (`internal/db`, config `/etc/patrabahok/mysql-admin.cnf`). It does not go through the API. `internal/api` (JSON, bearer tokens from `internal/authtoken`, Unix socket `/run/patrabahok/api.sock` by default) and `internal/webui` expose the same logic. A new operation usually needs wiring in all three places: CLI command, API handler, dashboard handler/template.
- `patrabahokd` runs four independent listeners: the local API, the group policy service (`-policy-addr`, `internal/policy`), the dashboard (HTTPS `:8443`, only when `-web-cert`/`-web-key` are given, reusing the mail server's Let's Encrypt cert), and the public `:443` listener (`-https-addr`, non-fatal on failure; `internal/frontdoor`). The public listener routes `mta-sts.*` hosts to `internal/mtasts`, `/api/<segment containing a dot>/...` to the per-domain public API (`internal/publicapi`, keys from `internal/domainkey`, one per domain), and everything else to the webmail reverse proxy (`-webmail-upstream`). The dot rule is what keeps patrabahok Mail's own `/api/accounts`-style routes on the webmail.
- **Groups, forwarding, catch-all** are all Postfix virtual alias expansion from one SQL query (`templates/postfix/mysql-virtual-alias-maps.cf.tmpl`, which maps every mailbox to itself so catch-alls don't swallow real users). Group *posting policies* are enforced by `internal/policy`, a Postfix policy service on `127.0.0.1:10040`, wired by `lib/core/mailrouting.sh` (shared by phases 50 and 55). Postfix defers group mail if the service is down, so `ensure_current_patrabahokd` (`lib/core/patrabahokd.sh`) rebuilds an older binary before Postfix depends on new daemon features.
- Server operations from the dashboard (services, fail2ban, queue, logs) live in `internal/ops`: a fixed catalog of services and allowed actions per service, a fixed set of log sources, and validated IPs and queue IDs. Never pass request input to a command except through those validators. The `patrabahokd` sandbox (`ProtectSystem=strict`) has to list any path an operation writes in `ReadWritePaths` (currently the Postfix spool and `/etc/fail2ban/jail.d`).
- Page data structs embed `Base`. Never give them a field named like a `Base` field (`Title`, `Active`, ...): it shadows the layout's value and the page 500s. `render_test.go` checks this.
- Upgrades: the installer rebuilds the binaries when `VERSION` differs from state `cli_built_version`. Other completed phases do not re-run on upgrade unless forced (`--force-phase`) or their logic lives in an always-run step, as migrations and `55-mail-routing` do.
- Admin changes go through `audit.Store.Record` (dashboard `s.record`, public API `s.record`, CLI `auditCLI`). Never record flag values or passwords.
- Dashboard conventions (`internal/webui`): pages get their `Base` from `s.base(w, r, title, active, crumbs...)`, which also consumes the flash cookie. Mutating handlers call `done(w, r, url, msg)` (flash + `HX-Redirect`) or `fail`/`failErr` (plain-text body, shown as an error toast by `static/app.js`). Store validation errors are `mailbox.ValidationError` (safe to show); anything else is logged and shown generically. Templates share `layout.html`, `icons.html` (`{{icon "name"}}`) and `components.html`. Never put template values inside Alpine/JS expressions: use `data-*` attributes.
- There are two separate APIs. `internal/api` is local (Unix socket), uses server-wide scoped tokens, and can do everything. `internal/publicapi` faces the internet and each key is limited to one domain's mailboxes and aliases. Docs: `docs/API.md` and `docs/PUBLIC-API.md`.
- The dashboard is server-rendered `html/template` + htmx + Alpine.js with no build step and no CDN. Templates and static files are embedded with `go:embed` in `cli/web/assets.go`, so the binary is self-contained. Admin login is its own system (`internal/adminauth`: argon2id + sessions) and is separate from API tokens.
- `internal/secretkey` keeps symmetric encryption keys in `/etc/patrabahok/secrets.env` (used, for example, for the stored Cloudflare tokens in `internal/cloudflare`). `mail-client/src/lib/secretkey.ts` mirrors it in TypeScript; keep the two compatible.

## Non-obvious constraints (from ARCHITECTURE.md)

- The Rspamd milter is wired to Postfix over **TCP loopback `127.0.0.1:11332`, not a Unix socket**. A Unix socket hit an unexplained ENOENT on Ubuntu 22.04. Don't switch it back.
- Rspamd `use_esld` must be **`false`**. DKIM keys are stored per exact domain string, so subdomain signing must not fall back to the parent domain's key.
- Postscreen DNSBL lookups go through the local `unbound` resolver, which Spamhaus fair use requires.
- In a deployment, set webmail's `PATRABAHOK_IMAP_HOST`/`PATRABAHOK_SMTP_HOST` to the server's public mail hostname, not localhost (the code falls back to localhost). The TLS certificate only matches that hostname, and hostname verification is meant to stay on (`mail-client/src/lib/mailserver.ts`).
