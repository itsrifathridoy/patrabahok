# `patrabahok` CLI

Installed to `/usr/local/bin/patrabahok`. Must be run as root. Reads database credentials from
`/etc/patrabahok/mysql-admin.cnf` (written by the installer, mode 600).

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
```

The domain must already be added (`patrabahok domain add`) before adding a mailbox in it. If
`--password` isn't given, you'll be prompted twice (hidden input). Passwords are hashed with
`doveadm pw -s SHA512-CRYPT` before being stored — plaintext passwords are never written to disk
or logs.

`--quota` is stored per-mailbox in the database for future use, but is **not currently enforced**
— Dovecot enforces a single global default quota (1G) for all mailboxes via `quota_rule` in
`/etc/dovecot/conf.d/99-patrabahok.conf`. See [ROADMAP.md](ROADMAP.md).

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
patrabahok status     # dumps /etc/patrabahok/state.json
```

## A note on SQL safety

The CLI is a Bash script that builds SQL statements from validated, escaped input (domain/email
format is checked with a regex before use, and every interpolated value is escaped via a
`sql_escape` helper before being placed in a single-quoted SQL string) — it does not use true
parameterized/prepared queries, because the `mysql` command-line client doesn't support them.
This is adequate for the MVP's threat model (local root-only tool, strict input validation) but
a real prepared-statement layer is one of the reasons a Go CLI is on the roadmap.
