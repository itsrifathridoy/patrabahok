# Troubleshooting

## Installer fails partway through

Check `/var/log/patrabahok/install.log` and the phase-specific output above the failure. Fix the
underlying issue, then re-run the same install command — completed phases are skipped
automatically (`/etc/patrabahok/state.json` tracks this). To force one phase to re-run:

```
/opt/patrabahok/current/bin/patrabahok-installer install --force-phase <phase-name>
```

## "Certbot failed" / TLS issuance fails

- Confirm `dig +short A mail.example.com @1.1.1.1` returns this server's public IP. DNS
  propagation can take minutes to hours depending on your provider/TTL.
- Confirm port 80 is reachable from the internet (ufw allows it by default; check your cloud
  provider's network/security-group rules too, not just the server's own firewall).
- Certbot rate-limits repeated failures for the same hostname — wait before retrying if you've
  failed several times in a row.

## Outbound mail never arrives / "Loopback delivery test FAILED" in `verify`

- Check `/var/log/mail.log` for the specific rejection/bounce reason.
- Many cloud providers (AWS, GCP, Azure, DigitalOcean, etc.) block outbound TCP/25 by default
  on new accounts — the installer warns about this during preflight but can't fix it. You
  usually need to open a support ticket with your provider.
- `systemctl status postfix dovecot rspamd` — make sure all three are actually running.

## Mail is arriving but marked as spam by Gmail/Outlook

- Give DNS at least a few hours to propagate — DKIM/DMARC checks fail hard until the records are
  visible.
- `patrabahok dns show <domain>` and re-verify every record matches exactly (SPF/DKIM/DMARC are
  picky about exact formatting).
- A brand-new sending IP with no history is inherently more likely to be filtered at first
  regardless of correct configuration — this improves with legitimate sending volume over time
  (IP/domain reputation warm-up), not something the installer can fix directly.

## `patrabahok` CLI: "Not configured yet"

The installer hasn't completed the database phase yet, or `/etc/patrabahok/mysql-admin.cnf` was
deleted. Re-run the installer.

## fail2ban banned my own IP

```
fail2ban-client set <jail> unbanip <your-ip>
```

List active jails with `fail2ban-client status`.

## Local DNS resolution seems broken after install

The installer points `/etc/resolv.conf` at the local `unbound` resolver (`127.0.0.1`) and
disables `systemd-resolved`'s stub listener, as part of the DNSBL fair-use design (see
[ARCHITECTURE.md](ARCHITECTURE.md)). If this breaks something else on the server:

```
systemctl status unbound
dig @127.0.0.1 example.com     # should return an answer
journalctl -u unbound -n 50
```

If unbound itself is down, `systemctl restart unbound` and re-check.

## Where things live

| What | Where |
|---|---|
| Installer log | `/var/log/patrabahok/install.log` |
| Installer state | `/etc/patrabahok/state.json` |
| Secrets | `/etc/patrabahok/secrets.env`, `/etc/patrabahok/mysql-admin.cnf` |
| Mail data | `/var/mail/vhosts/<domain>/<user>/Maildir` |
| Mail logs | `/var/log/mail.log` |
| DKIM keys | `/var/lib/rspamd/dkim/` |
| TLS certs | `/etc/letsencrypt/live/<hostname>/` |
| DNS record dump | `/var/lib/patrabahok/dns-records/patrabahok-dns-<domain>.txt` |
