# `patrabahokd` API reference

`patrabahokd` is a systemd-managed local JSON API, built from the same Go module as the
`patrabahok` CLI (`cli/`) and linking the same business logic (`cli/internal/mailbox`) against the
database with parameterized queries. It exists for automation/integration use — anything you can
do with the CLI, you can do over this API. See [CLI.md](CLI.md) for the command-line equivalent
and [WEB-UI.md](WEB-UI.md) for the browser dashboard (a separate account system, not this API).

## Transport

By default `patrabahokd` listens on a Unix domain socket at `/run/patrabahok/api.sock`
(mode `0660`, group `patrabahok`) — not a network port. Reaching it requires either local root or
membership in the `patrabahok` group:

```sh
curl --unix-socket /run/patrabahok/api.sock \
     -H "Authorization: Bearer $TOKEN" \
     http://localhost/v1/domains
```

A TCP listener is available via `patrabahokd -tcp 127.0.0.1:8991` (a systemd unit override) for
setups where a Unix socket isn't practical. It's off by default and never intended to be exposed
beyond localhost — put it behind your own reverse proxy/tunnel if you need remote access, don't
bind it to a public interface. See [SECURITY.md](SECURITY.md) for the reasoning.

## Authentication

Every endpoint except `GET /healthz` requires:

```
Authorization: Bearer <token>
```

Tokens are created with `patrabahok api token create <name> [--scope ...]` (see
[CLI.md](CLI.md#api-tokens)). The plaintext token is printed once at creation and cannot be
recovered afterward — only its SHA-256 hash is stored. A missing/malformed header, or a token that
doesn't verify (wrong, revoked, or expired), gets:

```http
HTTP/1.1 401 Unauthorized
Content-Type: application/json

{"error": "missing bearer token"}
```

or `{"error": "invalid or revoked token"}`.

### Scopes

Each token carries one or more scopes (comma-separated at creation), or `*` for all:
`domain`, `mailbox`, `alias`, `dkim`, `dns`, `queue`, `status`. A scope covers both reading and
writing that resource in v1 — there's no separate read-only variant yet. A token that
authenticates but lacks the scope an endpoint requires gets:

```http
HTTP/1.1 403 Forbidden
Content-Type: application/json

{"error": "token lacks required scope: mailbox"}
```

## Conventions

- All request and response bodies are JSON; requests that take a body must send
  `Content-Type: application/json` (not enforced strictly, but the body is always decoded as
  JSON).
- A malformed JSON body → `400 Bad Request`, `{"error": "invalid JSON body"}`.
- Validation failures (bad email format, non-positive quota, domain not registered, etc.) →
  `400 Bad Request` with a specific `error` message.
- Referencing something that doesn't exist (a domain, a mailbox) →
  `404 Not Found`, `{"error": "not found: ...detail..."}`.
- Successful writes that create a resource → `201 Created` with a small JSON body.
- Successful writes with nothing to return (delete, password/quota change) →
  `204 No Content`, empty body.
- Reads → `200 OK` with the resource as JSON.

There is currently no pagination — list endpoints return every row for the given filter in one
response.

---

## Health

### `GET /healthz`

No authentication required.

```http
GET /healthz

200 OK
{"status": "ok"}
```

## Status

### `GET /v1/status` — scope: `status`

Dumps `/etc/patrabahok/state.json` as-is: installer version, per-phase status, and persisted
config values. The shape is whatever the installer has written, not a fixed schema — treat new
keys as possible.

```http
GET /v1/status

200 OK
{
  "version": "0.1.0",
  "phases": {
    "10-packages": {"status": "done", "at": "2026-08-02T10:14:03Z"},
    "80-dkim-dmarc-dns": {"status": "done", "at": "2026-08-02T10:19:47Z"}
  },
  "config": {
    "mail_hostname": "mail.example.com",
    "domains": ["example.com"]
  }
}
```

## Domains

### `GET /v1/domains` — scope: `domain`

```http
GET /v1/domains

200 OK
[
  {"id": 1, "name": "example.com"},
  {"id": 2, "name": "example.org"}
]
```

### `POST /v1/domains` — scope: `domain`

Provisions a DKIM key and DNS record dump for the domain as part of adding it (the same shared
path the CLI and dashboard use).

```http
POST /v1/domains
Content-Type: application/json

{"name": "example.com"}

201 Created
{"name": "example.com"}
```

### `DELETE /v1/domains/{name}` — scope: `domain`

Cascades to the domain's mailboxes and aliases via a DB foreign key. Does **not** delete mail data
on disk — remove `/var/mail/vhosts/<domain>` yourself if you want that gone too.

```http
DELETE /v1/domains/example.com

204 No Content
```

## Mailboxes

### `GET /v1/mailboxes?domain=example.com` — scope: `mailbox`

`domain` is optional; omit it to list every mailbox on the server.

```http
GET /v1/mailboxes?domain=example.com

200 OK
[
  {"email": "alice@example.com", "enabled": true, "quota_bytes": 1073741824},
  {"email": "bob@example.com", "enabled": true, "quota_bytes": 2147483648}
]
```

### `POST /v1/mailboxes` — scope: `mailbox`

`quota_bytes` defaults to `1073741824` (1G) if omitted or `<= 0`. The password is hashed
(`doveadm pw -s SHA512-CRYPT`) before it ever reaches the database — never stored or logged in
plaintext.

```http
POST /v1/mailboxes
Content-Type: application/json

{"email": "alice@example.com", "password": "correct horse battery staple", "quota_bytes": 2147483648}

201 Created
{"email": "alice@example.com"}
```

### `DELETE /v1/mailboxes/{email}` — scope: `mailbox`

```http
DELETE /v1/mailboxes/alice@example.com

204 No Content
```

### `PUT /v1/mailboxes/{email}/password` — scope: `mailbox`

```http
PUT /v1/mailboxes/alice@example.com/password
Content-Type: application/json

{"password": "a new correct horse battery staple"}

204 No Content
```

### `PUT /v1/mailboxes/{email}/quota` — scope: `mailbox`

Accepts either an exact byte count or a human-readable size string (`"2G"`, `"500M"`) — send
whichever is convenient, not both. Genuinely enforced by Dovecot, not just stored: the change
takes effect immediately (no restart), since `dovecot-sql.conf.ext`'s `user_query` returns the
current `quota_bytes` as Dovecot's `quota_rule` on every session/delivery. See
[CLI.md](CLI.md#mailboxes) for the LMTP-rejection behavior once a mailbox is actually over quota.

```http
PUT /v1/mailboxes/alice@example.com/quota
Content-Type: application/json

{"quota": "2G"}

204 No Content
```

equivalently:

```http
PUT /v1/mailboxes/alice@example.com/quota
Content-Type: application/json

{"quota_bytes": 2147483648}

204 No Content
```

An unparseable `quota` string → `400 Bad Request` with the parse error as `error`.

## Aliases

### `GET /v1/aliases?domain=example.com` — scope: `alias`

`domain` is optional; omit it to list every alias on the server.

```http
GET /v1/aliases?domain=example.com

200 OK
[
  {"source": "info@example.com", "destination": "alice@example.com"}
]
```

### `POST /v1/aliases` — scope: `alias`

```http
POST /v1/aliases
Content-Type: application/json

{"source": "info@example.com", "destination": "alice@example.com"}

201 Created
{"source": "info@example.com", "destination": "alice@example.com"}
```

### `DELETE /v1/aliases` — scope: `alias`

Unlike the other delete endpoints, this one takes its target as a JSON body rather than a path
segment, since an alias is identified by the (source, destination) pair, not a single key.

```http
DELETE /v1/aliases
Content-Type: application/json

{"source": "info@example.com", "destination": "alice@example.com"}

204 No Content
```

## DKIM / DNS

Both of these read files the installer (or `DomainAdd`) already generated on disk — a domain with
no DKIM key yet (never added) returns `404`.

### `GET /v1/dkim/{domain}` — scope: `dkim`

```http
GET /v1/dkim/example.com

200 OK
{"record": "mail._domainkey.example.com. IN TXT \"v=DKIM1; k=rsa; p=MIGfMA0...\""}
```

### `GET /v1/dns/{domain}` — scope: `dns`

Returns the full record set dump (MX/SPF/DKIM/DMARC/MTA-STS) as one text blob, not split into
fields — see the dashboard's DNS Analysis page or `patrabahok dns show` for a parsed, per-record
view.

```http
GET /v1/dns/example.com

200 OK
{"records": "; DNS records for example.com\nMX  10 mail.example.com.\n..."}
```

## Mail queue

### `GET /v1/queue` — scope: `queue`

Raw `postqueue -p` output as a single string.

```http
GET /v1/queue

200 OK
{"queue": "-Queue ID- --Size-- ----Arrival Time---- -Sender/Recipient-------\nMailq is empty"}
```

### `POST /v1/queue/flush` — scope: `queue`

Raw `postqueue -f` output.

```http
POST /v1/queue/flush

200 OK
{"result": ""}
```

---

## Error shape reference

Every error response has this shape:

```json
{"error": "human-readable message"}
```

| Status | When |
|---|---|
| `400 Bad Request` | Malformed JSON body, or a valid request that fails validation (bad email, non-positive quota, unparseable quota string, domain not registered) |
| `401 Unauthorized` | Missing/malformed `Authorization` header, or a token that doesn't verify |
| `403 Forbidden` | Token verifies but lacks the endpoint's required scope |
| `404 Not Found` | The referenced domain/mailbox doesn't exist, or no DKIM/DNS record file exists for it yet |
| `500 Internal Server Error` | Something failed server-side unrelated to the request itself (e.g. the database is unreachable) |
