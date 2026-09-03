# Install

## Requirements

- Ubuntu 24.04 LTS, run as root (fresh server strongly recommended — the installer takes over
  Postfix/Dovecot/MariaDB/firewall configuration)
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

The installer checks DNS (A record for the hostname, MX for the domain) before proceeding and
will pause if they're not set yet, so you can add them and retry without restarting.

## What happens

Phases run in order, each recorded in `/etc/patrabahok/state.json` so a re-run skips whatever
already succeeded and resumes at the first failed/incomplete phase:

`preflight → packages → firewall → database → tls → postfix → dovecot → rspamd/clamav → dkim/dns → cli → verify`

At the end, DNS records are printed and saved to `/root/patrabahok-dns-<domain>.txt`. Add them,
wait for propagation, then send yourself a test email.

## Non-interactive / scripted install

```
cat > /root/patrabahok-answers.env <<'EOF'
PATRABAHOK_DOMAIN=example.com
PATRABAHOK_MAIL_HOSTNAME=mail.example.com
PATRABAHOK_ADMIN_EMAIL=postmaster@example.com
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

## Upgrading

Re-run the one-liner. It re-resolves the latest release, verifies its checksum, and re-invokes
the installer, which skips already-completed phases and only applies what's changed.

```
curl -sSL https://patrabahok.com/install.sh | sh -s -- --yes
```

Pin a specific version instead of latest with `--version vX.Y.Z`.
