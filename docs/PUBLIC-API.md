# Public per-domain API

An internet-facing HTTPS API for managing **one domain's** mailboxes and aliases. It is meant for
server-to-server integration, for example a school's student-information system creating a
mailbox for every new student:

```sh
curl -X POST https://mail.example.edu/api/example.edu/mailbox \
     -H "Authorization: Bearer $API_KEY" \
     -H "Content-Type: application/json" \
     -d '{"username": "student1", "password": "s3cret-pass", "name": "Student One"}'
```

This is a separate API from the local [`patrabahokd` API](API.md). The local one listens on a Unix
socket, uses server-wide tokens and can manage everything (domains, DKIM, queue). This one is
reachable from the internet, but each key can only manage its own domain's mailboxes and aliases.

## Transport

`patrabahokd` serves it on the mail server's public HTTPS port, `https://<mail hostname>/api/`,
with the same Let's Encrypt certificate as IMAP/SMTP. It also answers on the dashboard port
(`https://<mail hostname>:8443/api/`) with identical behavior. The same `:443` listener also serves the
webmail and MTA-STS policy files (see [ARCHITECTURE.md](ARCHITECTURE.md#public-https-listener-443)).

## API keys

Each domain has at most one API key. Create one in the dashboard (**Domains → Public API key →
Generate key**) or with the CLI:

```sh
patrabahok api key generate example.edu   # prints the key once
patrabahok api key list                    # domain, last characters of the key, last use
patrabahok api key revoke example.edu
```

The key (`pbd_` plus 64 hex characters) is shown once and can't be recovered, because only its
SHA-256 hash is stored. Generating a new key for a domain immediately invalidates the old one, so
that's also how you rotate a key. Deleting a domain deletes its key.

Send the key in either header:

```
Authorization: Bearer pbd_...
X-API-Key: pbd_...
```

| Situation | Response |
|---|---|
| No key | `401` `{"error": "missing API key ..."}` |
| Wrong or revoked key | `401` `{"error": "invalid or revoked API key"}` |
| Valid key, but for a different domain than the URL | `403` `{"error": "this API key is not valid for domain ..."}` |

Keep the key on your backend. The API sends no CORS headers, so browsers can't call it directly,
and a key in front-end code would hand mailbox control to anyone who opens the page.

## Conventions

- Request and response bodies are JSON (request bodies up to 64 KiB). Errors are always
  `{"error": "<message>"}`.
- `{domain}` in the path is the domain the key belongs to. `{username}` is the local part,
  `student1` in `student1@example.edu`.
- **Usernames**: 1–64 characters of lowercase letters, digits, `.`, `_` or `-`, starting and ending
  with a letter or digit. Uppercase input is lowercased.
- **Passwords**: 8–128 characters.
- **Names** (display names): one line, at most 255 characters. Stored for your reference and
  shown in the dashboard. Mail clients don't use it.
- **Quotas**: strings such as `"500M"` or `"2G"`. The default is `1G`.

A mailbox is always returned in this shape:

```json
{
  "email": "student1@example.edu",
  "username": "student1",
  "name": "Student One",
  "enabled": true,
  "quota_bytes": 1073741824
}
```

## Mailboxes

### `POST /api/{domain}/mailbox`: create a mailbox

```json
{"username": "student1", "password": "s3cret-pass", "name": "Student One", "quota": "1G"}
```

`name` and `quota` are optional. Returns `201` with the mailbox. If the mailbox already exists
it returns `409` and leaves the existing mailbox, including its password, untouched.

### `GET /api/{domain}/mailbox`: list mailboxes

Returns `200` with an array of mailboxes (possibly empty).

### `GET /api/{domain}/mailbox/{username}`: get one mailbox

Returns `200` with the mailbox, or `404` if it doesn't exist.

### `PUT /api/{domain}/mailbox/{username}/password`: change password

```json
{"password": "new-s3cret-pass"}
```

Returns `200` with the mailbox. Takes effect on the next login. Sessions that are already logged
in stay open until they reconnect.

### `PUT /api/{domain}/mailbox/{username}/name`: change display name

```json
{"name": "Student Uno"}
```

Returns `200` with the mailbox. Send `""` to clear it.

### `PUT /api/{domain}/mailbox/{username}/quota`: change quota

```json
{"quota": "2G"}
```

Returns `200` with the mailbox. Takes effect on the next login or delivery.

### `PUT /api/{domain}/mailbox/{username}/status`: enable or disable

```json
{"enabled": false}
```

Returns `200` with the mailbox. A disabled mailbox can't log in and doesn't accept new mail, but
its stored mail is kept. Re-enabling restores it as it was.

### `DELETE /api/{domain}/mailbox/{username}`: delete a mailbox

Returns `204`, or `404` if it doesn't exist. **This permanently deletes the mailbox's stored
mail.** Use `status` to suspend a mailbox instead.

### `PUT /api/{domain}/mailbox/{username}/forward`: forward incoming mail

```json
{"destination": "student1.personal@gmail.com", "keep_copy": true}
```

`keep_copy` defaults to `true`. With `false`, mail is only delivered to the destination. Returns
`200` with `{"destination": ..., "keep_copy": ...}`.

### `DELETE /api/{domain}/mailbox/{username}/forward`: turn forwarding off

Returns `204`.

## Groups

A group is one address (`class10@example.edu`) that delivers to every member. Members can be
mailboxes, aliases, other groups (groups nest), or, if the group allows it, external addresses.
Who may send to the group is set by its posting policy, which is checked when a message
arrives. Senders who aren't allowed get an immediate SMTP error.

| `post_policy` | Who can post |
|---|---|
| `anyone` | Anyone, including the internet |
| `organization` (default) | Any signed-in mailbox on this server |
| `members` | The group's members and owners |
| `owners` | Only owners. Use this for announcement lists |

People on this server must be signed in (authenticated SMTP) to post to a restricted group. An
unauthenticated message claiming to be from a local address is rejected, because that's trivially
spoofable. External members are recognized by their envelope sender.

A group is returned as:

```json
{
  "email": "class10@example.edu",
  "name": "Class 10",
  "description": "All Class 10 students",
  "post_policy": "members",
  "allow_external": true,
  "member_count": 2,
  "created_at": "2026-10-24T08:13:29Z",
  "members": [
    {"address": "teacher@example.edu", "role": "owner", "kind": "user", "name": "Ms. Rahman", "added_at": "..."},
    {"address": "parent@gmail.com", "role": "member", "kind": "external", "added_at": "..."}
  ]
}
```

`members` is only included when getting a single group. `kind` is `user`, `group`, `alias`,
`external`, or `missing` (a local address that no longer exists). Suspended users are marked
`"suspended": true` and don't receive group mail.

### `GET /api/{domain}/group`: list groups

### `POST /api/{domain}/group`: create a group

```json
{"name": "class10", "display_name": "Class 10", "description": "…", "post_policy": "members", "allow_external": false}
```

`name` is the local part of the group's address. Everything else is optional. Returns `201`.
Returns `409` if the address is already used by a mailbox, alias or group.

### `GET /api/{domain}/group/{name}`: get a group with its members

### `PUT /api/{domain}/group/{name}`: change settings

Takes the same fields as create, except `name`. Omitted fields keep their current value. Turning
off `allow_external` fails while external members remain.

### `DELETE /api/{domain}/group/{name}`: delete a group

Returns `204`. Members' mailboxes aren't affected.

### `POST /api/{domain}/group/{name}/members`: add members

```json
{"members": ["s1@example.edu", "s2@example.edu", "parent@gmail.com"], "role": "member"}
```

`role` is `member` (the default) or `owner`. Up to 1000 addresses per request. Each address
succeeds or fails on its own, so one bad address doesn't block the rest:

```json
{"added": 2, "failed": 1, "results": [
  {"address": "s1@example.edu", "added": true},
  {"address": "s2@example.edu", "added": true},
  {"address": "parent@gmail.com", "added": false, "error": "external addresses aren't allowed in this group (change it in the group's settings)"}
]}
```

Adding an existing member again updates their role.

### `DELETE /api/{domain}/group/{name}/members/{address}`: remove a member

Returns `204`, or `404` if the address isn't a member.

## Aliases

An alias forwards `source@{domain}` to any address, inside or outside this server.

### `GET /api/{domain}/alias`: list aliases

```json
[{"source": "info@example.edu", "destination": "student1@example.edu"}]
```

### `POST /api/{domain}/alias`: add an alias

```json
{"source": "info", "destination": "student1@example.edu"}
```

`source` is a local part within `{domain}`. Returns `201` with the alias. Adding one that already
exists is a no-op that also returns `201`.

### `DELETE /api/{domain}/alias`: remove an alias

Same body as adding one. Returns `204`.

## Status codes

| Code | Meaning |
|---|---|
| 200 / 201 / 204 | Success |
| 400 | Invalid JSON, username, password, name or quota (the message says which) |
| 401 / 403 | See [API keys](#api-keys) |
| 404 | No such mailbox or endpoint |
| 409 | Mailbox already exists |
| 500 | Server-side failure (details are in `journalctl -u patrabahokd`, not in the response) |
