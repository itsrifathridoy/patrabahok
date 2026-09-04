# `patrabahok` CLI and `patrabahokd` API

`patrabahok` (installed to `/usr/local/bin/patrabahok`) and `patrabahokd` (a systemd-managed
daemon) are both built from the Go module at `cli/` (`go build ./cmd/patrabahok`,
`./cmd/patrabahokd`) by the installer's `95-cli` phase. Both link the same business logic
(`cli/internal/mailbox`) against the database with parameterized queries — no string-built SQL.
`patrabahok` must be run as root; it reads database credentials from
`/etc/patrabahok/mysql-admin.cnf` (mode 600, written by the installer).

## Domains

```
patrabahok domain add <domain>
patrabahok domain list
patrabahok domain remove <domain> [--force]
```

Removing a domain cascades (deletes its mailboxes and aliases via a DB foreign key) but does
**not** delete mail data on disk — remove `/var/mail/vhosts/<domain>` manually if you want that
gone too.

## Mailboxes

```
patrabahok mailbox add <user@domain> [--quota 500M] [--password PASS]
patrabahok mailbox list [domain]
patrabahok mailbox remove <user@domain> [--force]
patrabahok mailbox passwd <user@domain> [--password PASS]
patrabahok mailbox quota <user@domain> <quota>    # e.g. 2G, 500M
```

The domain must already be added (`patrabahok domain add`) before adding a mailbox in it. If
`--password` isn't given, you'll be prompted twice (hidden input, via `golang.org/x/term`).
Passwords are hashed with `doveadm pw -s SHA512-CRYPT` (invoked via `argv`, never through a
shell) before being stored — plaintext passwords are never written to disk or logs.

`--quota`/`mailbox quota` set `quota_bytes` per mailbox in the database, and it's genuinely
enforced by Dovecot — not just stored. `dovecot-sql.conf.ext`'s `user_query` returns
`quota_bytes` as Dovecot's `quota_rule` extra userdb field on every login/delivery, so each
mailbox gets its own limit instead of everyone sharing one static value. A `quota mailbox`
change takes effect immediately (no restart, no cache to invalidate) — Dovecot reads it fresh on
the mailbox's next IMAP session or delivery. Once a mailbox is actually over quota, LMTP delivery
is rejected (`552 5.2.2 Quota exceeded`) and Postfix bounces the message back to the sender —
verify current usage/limit with `doveadm quota get -u <user@domain>`.

## Aliases

```
patrabahok alias add <alias@domain> <target@domain>
patrabahok alias list [domain]
patrabahok alias remove <alias@domain> <target@domain>
```

## DKIM / DNS

```
patrabahok dkim show <domain>     # prints the DKIM DNS TXT record
patrabahok dns show <domain>      # prints the full record set (MX/SPF/DKIM/DMARC/MTA-STS)
```

## Mail queue

```
patrabahok queue list
patrabahok queue flush
```

## Installer state

```
patrabahok status     # dumps /etc/patrabahok/state.json as JSON
```

## API tokens

```
patrabahok api token create <name> [--scope domain,mailbox,alias,dkim,dns,queue,status]
patrabahok api token list
patrabahok api token revoke <name>
```

`--scope` defaults to `*` (full access) if omitted. The plaintext token is printed once at
creation and cannot be recovered afterward — only its SHA-256 hash is stored (see
[SECURITY.md](SECURITY.md)).

## The `patrabahokd` API

A local JSON API mirroring the CLI, for automation/integration use. Listens on a Unix domain
socket at `/run/patrabahok/api.sock` by default (mode 0660, group `patrabahok`) — not a network
port — so reaching it requires either local root or membership in the `patrabahok` group. Every
request needs `Authorization: Bearer <token>` from `patrabahok api token create`.

```
GET    /healthz                          # no auth
GET    /v1/status
GET    /v1/domains
POST   /v1/domains                       {"name": "..."}
DELETE /v1/domains/{name}
GET    /v1/mailboxes?domain=...
POST   /v1/mailboxes                     {"email": "...", "password": "...", "quota_bytes": 1073741824}
DELETE /v1/mailboxes/{email}
PUT    /v1/mailboxes/{email}/password    {"password": "..."}
PUT    /v1/mailboxes/{email}/quota       {"quota": "2G"} or {"quota_bytes": 2147483648}
GET    /v1/aliases?domain=...
POST   /v1/aliases                       {"source": "...", "destination": "..."}
DELETE /v1/aliases                       {"source": "...", "destination": "..."}
GET    /v1/dkim/{domain}
GET    /v1/dns/{domain}
GET    /v1/queue
POST   /v1/queue/flush
```

Example call over the socket with curl:

```
curl --unix-socket /run/patrabahok/api.sock \
     -H "Authorization: Bearer $TOKEN" \
     http://localhost/v1/domains
```

A TCP listener is available via `patrabahokd -tcp 127.0.0.1:8991` (systemd unit override) for
setups where a Unix socket isn't practical — off by default, and never intended to be exposed
beyond localhost. See [SECURITY.md](SECURITY.md) for the reasoning.

## Scopes

Each token has one or more scopes (comma-separated), or `*` for all: `domain`, `mailbox`,
`alias`, `dkim`, `dns`, `queue`, `status`. A scope covers both reading and writing that resource
in v1 — finer read/write splitting is a possible future refinement, not implemented yet.

## Web dashboard admins

```
patrabahok webadmin add <username> [--password PASS]
patrabahok webadmin list
patrabahok webadmin remove <username>
```

Manages accounts for the browser dashboard at `https://<hostname>:8443/` — a separate account
system from API tokens (these are real username/password logins, argon2id-hashed). The first
account is created automatically during install; see [WEB-UI.md](WEB-UI.md).
