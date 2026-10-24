# Install

## Requirements

- Ubuntu 24.04, Ubuntu 22.04, or Debian 12, run as root (fresh server strongly recommended —
  the installer takes over Postfix/Dovecot/MariaDB/firewall configuration)
- A domain you control
- Ability to add DNS records (A, MX, TXT) for that domain
- Outbound TCP/25 not blocked (common on some cloud providers for new accounts — the installer
  checks this and warns, but can't fix it for you; you may need a support ticket)

## Interactive install

```
curl -sSL https://patrabahok.com/install.sh | sh -s -- --yes
```

`--yes` is required for the piped form (`curl | sh`) — it's the installer's way of making sure
you meant to run a script you piped straight into a root shell. If you'd rather read it first:

```
curl -fsSLO https://patrabahok.com/install.sh
less install.sh
sh install.sh --yes
```

You'll be asked for:
- **Primary mail domain** (e.g. `example.com`)
- **Mail server hostname** (e.g. `mail.example.com`, defaults to `mail.<domain>`)
- **Admin email** (used for Let's Encrypt, postmaster, DMARC aggregate reports)
- **Webmail client**, served at `https://<mail hostname>/`:
  - `patrabahok` (the default): patrabahok Webmail, a redesigned client built on the
    [Roundcube](https://roundcube.net) 1.7 engine (`webmail/roundcube/`). It has a new interface
    (light and dark), a modern composer with undo send, emoji and focus mode, a command palette
    (Ctrl/Cmd+K), Gmail-style keyboard shortcuts, quick filters, hover actions and avatars, plus
    every Roundcube feature: contacts, filters and vacation replies (ManageSieve), password
    change, archive, junk reporting, zip download and new-mail notifications.
  - `roundcube`: the same engine with Roundcube's stock Elastic look.

    Both run on nginx + PHP-FPM on loopback only, and failed logins are banned by fail2ban.
  - `custom`: the experimental Next.js client (`mail-client/`, Node.js + PostgreSQL).
  - `none`: no webmail. Users connect with desktop or mobile mail apps.

The installer checks DNS (A record for the hostname, MX for the domain) before proceeding and
will pause if they're not set yet, so you can add them and retry without restarting.

## What happens

Phases run in order, each recorded in `/etc/patrabahok/state.json` so a re-run skips whatever
already succeeded and resumes at the first failed/incomplete phase:

`preflight → packages → firewall → database → tls → postfix → mail routing (groups) → dovecot → rspamd/clamav → dkim/dns → webmail → cli → verify`

At the end, DNS records are printed and saved to
`/var/lib/patrabahok/dns-records/patrabahok-dns-<domain>.txt`. Add them, wait for propagation,
then send yourself a test email.

## Non-interactive / scripted install

```
cat > /root/patrabahok-answers.env <<'EOF'
PATRABAHOK_DOMAIN=example.com
PATRABAHOK_MAIL_HOSTNAME=mail.example.com
PATRABAHOK_ADMIN_EMAIL=postmaster@example.com
PATRABAHOK_WEBMAIL=patrabahok  # patrabahok (default), roundcube, custom, or none
EOF

curl -sSL https://patrabahok.com/install.sh | sh -s -- --yes --non-interactive --config /root/patrabahok-answers.env
```

In `--non-interactive` mode, any required value that isn't set via `PATRABAHOK_<VAR>` (env or
`--config` file) causes the installer to list every missing value at once and exit, rather than
hanging on a prompt.

## Resuming after a failure

Re-run the same command. Completed phases are skipped; the installer picks up at the phase that
failed. To force one specific phase to re-run (e.g. after manually fixing something):

```
/opt/patrabahok/current/bin/patrabahok-installer install --force-phase 50-postfix
```

## Adding more domains and mailboxes later

Don't re-run the whole installer — use the CLI (see [CLI.md](CLI.md)):

```
patrabahok domain add another-example.com
patrabahok mailbox add you@another-example.com
patrabahok dns show another-example.com
```

To let another system create and manage a domain's mailboxes over HTTPS, generate that domain's
API key (dashboard **Domains** page, or `patrabahok api key generate <domain>`). See
[PUBLIC-API.md](PUBLIC-API.md).

## Changing the webmail client

Re-run the webmail phase with the new choice. The previous client is disabled, but its database
is kept:

```
PATRABAHOK_WEBMAIL=patrabahok /opt/patrabahok/current/bin/patrabahok-installer install --force-phase 90-webmail
```

Switching between `patrabahok` and `roundcube` keeps every user's settings, contacts and
filters, because they share one Roundcube database.

## Upgrading

Re-run the one-liner. It re-resolves the latest release, verifies its checksum, and re-invokes
the installer, which skips already-completed phases and only applies what's changed.

```
curl -sSL https://patrabahok.com/install.sh | sh -s -- --yes
```

Pin a specific version instead of latest with `--version vX.Y.Z`.

Upgrading a server installed before the webmail phase existed runs `90-webmail` for the first
time, which asks for a webmail client (in `--non-interactive` mode it uses `PATRABAHOK_WEBMAIL`,
defaulting to `patrabahok`). It also rebuilds `patrabahokd` so the public API and webmail are
served on `:443`.
