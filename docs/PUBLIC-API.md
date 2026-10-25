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

## Quick start

1. **Create a key** for your domain: dashboard → **Domains** → your domain → **Public API key →
   Generate key**, or `patrabahok api key generate example.edu` on the server. Copy it now; it's
   shown only once.
2. **Store it as a secret** in the application that will call the API, for example an
   environment variable `PATRABAHOK_API_KEY`, never in source code or front-end JavaScript.
3. **Point the application at** `https://<mail hostname>/api/<your domain>/` (for example
   `https://mail.example.edu/api/example.edu/`).
4. **Check the connection** by listing mailboxes. `200` means the key works:

   ```sh
   curl -s https://mail.example.edu/api/example.edu/mailbox -H "Authorization: Bearer $PATRABAHOK_API_KEY"
   ```

Then use one of the [ready-made clients](#client-libraries) below, or call the endpoints directly.

## Endpoint summary

| Method | Path (under `/api/{domain}`) | Does | Success |
|---|---|---|---|
| `POST` | `/mailbox` | Create a mailbox | `201` mailbox |
| `GET` | `/mailbox` | List mailboxes | `200` array |
| `GET` | `/mailbox/{username}` | Get a mailbox | `200` mailbox |
| `PUT` | `/mailbox/{username}/password` | Change password | `200` mailbox |
| `PUT` | `/mailbox/{username}/name` | Change display name | `200` mailbox |
| `PUT` | `/mailbox/{username}/quota` | Change quota | `200` mailbox |
| `PUT` | `/mailbox/{username}/status` | Enable / disable | `200` mailbox |
| `PUT` | `/mailbox/{username}/forward` | Forward incoming mail | `200` |
| `DELETE` | `/mailbox/{username}/forward` | Stop forwarding | `204` |
| `DELETE` | `/mailbox/{username}` | Delete mailbox **and its mail** | `204` |
| `GET` | `/group` | List groups | `200` array |
| `POST` | `/group` | Create a group | `201` group |
| `GET` | `/group/{name}` | Get a group with members | `200` group |
| `PUT` | `/group/{name}` | Change group settings | `200` group |
| `DELETE` | `/group/{name}` | Delete a group | `204` |
| `POST` | `/group/{name}/members` | Add members (bulk) | `200` per-address results |
| `DELETE` | `/group/{name}/members/{address}` | Remove a member | `204` |
| `GET` | `/alias` | List aliases | `200` array |
| `POST` | `/alias` | Add an alias | `201` alias |
| `DELETE` | `/alias` | Remove an alias | `204` |

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
| 404 | No such mailbox, group, member or endpoint |
| 409 | The address is already taken (mailbox, alias or group) |
| 500 | Server-side failure (details are in `journalctl -u patrabahokd`, not in the response) |

## Integration guide

### Client libraries

Each of these is a complete, dependency-light client you can copy into your application. They
send the key, decode JSON, and turn API errors into exceptions that carry the HTTP status and the
server's message.

#### JavaScript / TypeScript (Node.js 18+, Deno, Bun; uses `fetch`)

```js
// patrabahok.js
export class PatrabahokError extends Error {
  constructor(status, message) { super(message); this.status = status; }
}

export class Patrabahok {
  /** @param {{host: string, domain: string, apiKey: string, timeoutMs?: number}} opts */
  constructor({ host, domain, apiKey, timeoutMs = 15000 }) {
    this.base = `https://${host}/api/${encodeURIComponent(domain)}`;
    this.apiKey = apiKey;
    this.timeoutMs = timeoutMs;
  }

  async request(method, path, body) {
    const res = await fetch(this.base + path, {
      method,
      headers: {
        Authorization: `Bearer ${this.apiKey}`,
        ...(body !== undefined && { 'Content-Type': 'application/json' }),
      },
      body: body !== undefined ? JSON.stringify(body) : undefined,
      signal: AbortSignal.timeout(this.timeoutMs),
    });
    if (res.status === 204) return null;
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new PatrabahokError(res.status, data.error || res.statusText);
    return data;
  }

  // mailboxes
  listMailboxes()                  { return this.request('GET', '/mailbox'); }
  getMailbox(u)                    { return this.request('GET', `/mailbox/${encodeURIComponent(u)}`); }
  createMailbox(u, password, name, quota) {
    return this.request('POST', '/mailbox', { username: u, password, name, quota });
  }
  setPassword(u, password)         { return this.request('PUT', `/mailbox/${encodeURIComponent(u)}/password`, { password }); }
  setName(u, name)                 { return this.request('PUT', `/mailbox/${encodeURIComponent(u)}/name`, { name }); }
  setQuota(u, quota)               { return this.request('PUT', `/mailbox/${encodeURIComponent(u)}/quota`, { quota }); }
  setEnabled(u, enabled)           { return this.request('PUT', `/mailbox/${encodeURIComponent(u)}/status`, { enabled }); }
  setForward(u, destination, keep_copy = true) {
    return this.request('PUT', `/mailbox/${encodeURIComponent(u)}/forward`, { destination, keep_copy });
  }
  clearForward(u)                  { return this.request('DELETE', `/mailbox/${encodeURIComponent(u)}/forward`); }
  deleteMailbox(u)                 { return this.request('DELETE', `/mailbox/${encodeURIComponent(u)}`); }

  // groups
  listGroups()                     { return this.request('GET', '/group'); }
  getGroup(n)                      { return this.request('GET', `/group/${encodeURIComponent(n)}`); }
  createGroup(group)               { return this.request('POST', '/group', group); }
  updateGroup(n, changes)          { return this.request('PUT', `/group/${encodeURIComponent(n)}`, changes); }
  deleteGroup(n)                   { return this.request('DELETE', `/group/${encodeURIComponent(n)}`); }
  addMembers(n, members, role = 'member') {
    return this.request('POST', `/group/${encodeURIComponent(n)}/members`, { members, role });
  }
  removeMember(n, address)         { return this.request('DELETE', `/group/${encodeURIComponent(n)}/members/${encodeURIComponent(address)}`); }

  // aliases
  listAliases()                    { return this.request('GET', '/alias'); }
  addAlias(source, destination)    { return this.request('POST', '/alias', { source, destination }); }
  removeAlias(source, destination) { return this.request('DELETE', '/alias', { source, destination }); }
}
```

```js
import { Patrabahok, PatrabahokError } from './patrabahok.js';

const mail = new Patrabahok({
  host: 'mail.example.edu',
  domain: 'example.edu',
  apiKey: process.env.PATRABAHOK_API_KEY,
});

try {
  const box = await mail.createMailbox('student1', 'Init-Pass-2026!', 'Student One', '2G');
  console.log('created', box.email);
} catch (e) {
  if (e instanceof PatrabahokError && e.status === 409) console.log('already exists');
  else throw e;
}
```

#### Python 3 (`requests`)

```python
# patrabahok.py
import requests
from urllib.parse import quote


class PatrabahokError(Exception):
    def __init__(self, status, message):
        super().__init__(f"{status}: {message}")
        self.status = status
        self.message = message


class Patrabahok:
    def __init__(self, host, domain, api_key, timeout=15):
        self.base = f"https://{host}/api/{quote(domain)}"
        self.timeout = timeout
        self.session = requests.Session()
        self.session.headers["Authorization"] = f"Bearer {api_key}"

    def request(self, method, path, body=None):
        r = self.session.request(method, self.base + path, json=body, timeout=self.timeout)
        if r.status_code == 204:
            return None
        try:
            data = r.json()
        except ValueError:
            data = {}
        if not r.ok:
            raise PatrabahokError(r.status_code, data.get("error", r.reason))
        return data

    # mailboxes
    def list_mailboxes(self):                 return self.request("GET", "/mailbox")
    def get_mailbox(self, u):                 return self.request("GET", f"/mailbox/{quote(u)}")
    def create_mailbox(self, u, password, name=None, quota=None):
        body = {"username": u, "password": password}
        if name is not None: body["name"] = name
        if quota is not None: body["quota"] = quota
        return self.request("POST", "/mailbox", body)
    def set_password(self, u, password):      return self.request("PUT", f"/mailbox/{quote(u)}/password", {"password": password})
    def set_name(self, u, name):              return self.request("PUT", f"/mailbox/{quote(u)}/name", {"name": name})
    def set_quota(self, u, quota):            return self.request("PUT", f"/mailbox/{quote(u)}/quota", {"quota": quota})
    def set_enabled(self, u, enabled):        return self.request("PUT", f"/mailbox/{quote(u)}/status", {"enabled": enabled})
    def set_forward(self, u, destination, keep_copy=True):
        return self.request("PUT", f"/mailbox/{quote(u)}/forward", {"destination": destination, "keep_copy": keep_copy})
    def clear_forward(self, u):               return self.request("DELETE", f"/mailbox/{quote(u)}/forward")
    def delete_mailbox(self, u):              return self.request("DELETE", f"/mailbox/{quote(u)}")

    # groups
    def list_groups(self):                    return self.request("GET", "/group")
    def get_group(self, n):                   return self.request("GET", f"/group/{quote(n)}")
    def create_group(self, **group):          return self.request("POST", "/group", group)
    def update_group(self, n, **changes):     return self.request("PUT", f"/group/{quote(n)}", changes)
    def delete_group(self, n):                return self.request("DELETE", f"/group/{quote(n)}")
    def add_members(self, n, members, role="member"):
        return self.request("POST", f"/group/{quote(n)}/members", {"members": members, "role": role})
    def remove_member(self, n, address):      return self.request("DELETE", f"/group/{quote(n)}/members/{quote(address)}")

    # aliases
    def list_aliases(self):                   return self.request("GET", "/alias")
    def add_alias(self, source, destination): return self.request("POST", "/alias", {"source": source, "destination": destination})
    def remove_alias(self, source, destination):
        return self.request("DELETE", "/alias", {"source": source, "destination": destination})
```

```python
import os
from patrabahok import Patrabahok, PatrabahokError

mail = Patrabahok("mail.example.edu", "example.edu", os.environ["PATRABAHOK_API_KEY"])
try:
    print(mail.create_mailbox("student1", "Init-Pass-2026!", "Student One")["email"])
except PatrabahokError as e:
    if e.status != 409:
        raise
```

#### PHP 8 (cURL; works in Laravel, WordPress, plain PHP)

```php
<?php
// Patrabahok.php
final class PatrabahokException extends RuntimeException {}

final class Patrabahok
{
    private string $base;

    public function __construct(string $host, string $domain, private string $apiKey, private int $timeout = 15)
    {
        $this->base = 'https://' . $host . '/api/' . rawurlencode($domain);
    }

    public function request(string $method, string $path, ?array $body = null): ?array
    {
        $ch = curl_init($this->base . $path);
        $headers = ['Authorization: Bearer ' . $this->apiKey, 'Accept: application/json'];
        if ($body !== null) {
            $headers[] = 'Content-Type: application/json';
            curl_setopt($ch, CURLOPT_POSTFIELDS, json_encode($body, JSON_THROW_ON_ERROR));
        }
        curl_setopt_array($ch, [
            CURLOPT_CUSTOMREQUEST => $method,
            CURLOPT_HTTPHEADER => $headers,
            CURLOPT_RETURNTRANSFER => true,
            CURLOPT_TIMEOUT => $this->timeout,
        ]);
        $raw = curl_exec($ch);
        if ($raw === false) {
            throw new PatrabahokException('connection failed: ' . curl_error($ch), 0);
        }
        $status = curl_getinfo($ch, CURLINFO_RESPONSE_CODE);
        curl_close($ch);
        if ($status === 204) {
            return null;
        }
        $data = json_decode($raw, true) ?? [];
        if ($status >= 400) {
            throw new PatrabahokException($data['error'] ?? "HTTP $status", $status);
        }
        return $data;
    }

    public function createMailbox(string $u, string $password, ?string $name = null, ?string $quota = null): array
    {
        return $this->request('POST', '/mailbox', array_filter(
            ['username' => $u, 'password' => $password, 'name' => $name, 'quota' => $quota],
            fn ($v) => $v !== null
        ));
    }
    public function getMailbox(string $u): array      { return $this->request('GET', '/mailbox/' . rawurlencode($u)); }
    public function listMailboxes(): array            { return $this->request('GET', '/mailbox'); }
    public function setPassword(string $u, string $p): array { return $this->request('PUT', '/mailbox/' . rawurlencode($u) . '/password', ['password' => $p]); }
    public function setName(string $u, string $n): array     { return $this->request('PUT', '/mailbox/' . rawurlencode($u) . '/name', ['name' => $n]); }
    public function setQuota(string $u, string $q): array    { return $this->request('PUT', '/mailbox/' . rawurlencode($u) . '/quota', ['quota' => $q]); }
    public function setEnabled(string $u, bool $on): array   { return $this->request('PUT', '/mailbox/' . rawurlencode($u) . '/status', ['enabled' => $on]); }
    public function deleteMailbox(string $u): void           { $this->request('DELETE', '/mailbox/' . rawurlencode($u)); }
    public function addMembers(string $group, array $members, string $role = 'member'): array
    {
        return $this->request('POST', '/group/' . rawurlencode($group) . '/members', ['members' => $members, 'role' => $role]);
    }
}
```

```php
$mail = new Patrabahok('mail.example.edu', 'example.edu', getenv('PATRABAHOK_API_KEY'));
try {
    $box = $mail->createMailbox('student1', 'Init-Pass-2026!', 'Student One');
} catch (PatrabahokException $e) {
    if ($e->getCode() !== 409) {
        throw $e;
    }
}
```

### Common workflows

**Provision a new user** (e.g. a student is enrolled):

1. Generate a strong initial password in your application (16+ random characters).
2. `POST /mailbox` with the username, password and name.
3. On `201`, hand the address and password to the person through your own channel (portal,
   printed slip) and ask them to change it in the webmail under **Settings → Password**.
4. On `409` the address already exists. If your records say this person owns it, decide whether
   to leave it or reset the password with `PUT /mailbox/{username}/password`.
5. Optionally add them to groups: `POST /group/{class}/members` with their address.

**Reset a forgotten password**: `PUT /mailbox/{username}/password`. The new password works at the
next login.

**Suspend and remove a user** (e.g. a student leaves): first `PUT /mailbox/{username}/status`
with `{"enabled": false}`. Mail is kept and the account can be restored. Only `DELETE` it once the
retention period your organization requires is over, because deleting erases their mail.

**Keep your system and the mail server in sync** (nightly job):

1. `GET /mailbox` returns every mailbox in the domain.
2. Compare it with your user list by `username`.
3. Create the missing ones, disable the ones that should no longer be active, and fix names and
   quotas with the `PUT` endpoints.

**Class or department lists**: create a group per class (`POST /group`, `post_policy: "members"`
so only the class can write to it, or `"owners"` for announcement-only lists), then add members
in batches of up to 1000 per request. Each address reports success or failure on its own.

### Retries, timeouts and concurrency

- **Safe to retry** after a timeout or a `5xx`: every `GET`, `PUT` and `DELETE`. They are
  idempotent; repeating one leaves the same result (a repeated `DELETE` may answer `404`, which
  you can treat as done).
- **`POST /mailbox`** is not idempotent. If a retry gets `409`, the first attempt most likely
  succeeded: confirm with `GET /mailbox/{username}`. **`POST /alias`** is idempotent (an existing
  alias returns `201`), and re-adding group members just updates their role.
- Use a client timeout around 15 seconds and retry `5xx` / network errors with backoff (for
  example 1 s, 4 s, 15 s). Never retry `400`, `401`, `403` or `409`; fix the request instead.
- The server doesn't rate-limit API requests, but it is your mail server too. Keep bulk jobs to a
  few requests at a time (sequential or up to about 4 in parallel) rather than thousands at once.

### Security checklist

- The key controls every mailbox in its domain. Treat it like a database password: keep it in a
  secret store or environment variable, out of source control, logs, error reports and browsers.
  The API sends no CORS headers on purpose, so it can only be called from a backend.
- Always use `https://`. Don't turn off certificate verification in your HTTP client; the server
  has a valid Let's Encrypt certificate.
- **Rotating the key**: generating a new key makes the old one stop working immediately. Update
  the application's secret right after generating, or do it in a short maintenance window. Revoke
  a key (`patrabahok api key revoke example.edu`, or in the dashboard) as soon as you suspect it
  leaked.
- Every API change is written to the audit log (dashboard → **Audit log**) with the domain key
  as the actor. Rejected keys are logged with the client IP (`journalctl -u patrabahokd`).
- Never put a user's password in a URL or a log line. The API only accepts passwords in the JSON
  body.

### Testing your integration

Use a test username you can delete afterwards:

```sh
API=https://mail.example.edu/api/example.edu
H="Authorization: Bearer $PATRABAHOK_API_KEY"

curl -sf -X POST "$API/mailbox" -H "$H" -H 'Content-Type: application/json' \
     -d '{"username":"api-test","password":"Test-Pass-2026!","name":"API Test"}'
curl -sf "$API/mailbox/api-test" -H "$H"
curl -sf -X PUT "$API/mailbox/api-test/status" -H "$H" -H 'Content-Type: application/json' -d '{"enabled":false}'
curl -sf -X DELETE "$API/mailbox/api-test" -H "$H" -o /dev/null -w '%{http_code}\n'   # 204
```
