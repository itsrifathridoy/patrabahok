# Backup & restore

There's no automated backup job installed yet (see [ROADMAP.md](ROADMAP.md)). Until then, here's
what to back up and how to restore it manually.

## What to back up

1. **Mail data**: `/var/mail/vhosts/` (Maildir per domain/user)
2. **Database**: the `mailserver` database in MariaDB
3. **Config**: `/etc/postfix/`, `/etc/dovecot/`, `/etc/rspamd/local.d/`, `/etc/unbound/unbound.conf.d/`,
   `/etc/fail2ban/jail.local`
4. **Secrets**: `/etc/patrabahok/` (contains `secrets.env` and `mysql-admin.cnf` — treat as
   highly sensitive, mode 600 already, keep backups encrypted)
5. **DKIM keys**: `/var/lib/rspamd/dkim/` — if you lose these without a backup, you must
   generate new keys and update the DNS TXT record for every domain (mail sent in the interim
   before DNS propagates may fail DKIM checks at strict receivers)
6. **TLS certificates**: `/etc/letsencrypt/` (can also just be re-issued if lost, since it's
   free and automated — lower priority than the above)

## A manual backup

```bash
BACKUP_DIR="/root/patrabahok-backup-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$BACKUP_DIR"

mysql --defaults-extra-file=/etc/patrabahok/mysql-admin.cnf -e "SELECT 1" >/dev/null \
  && mysqldump --defaults-extra-file=/etc/patrabahok/mysql-admin.cnf mailserver > "$BACKUP_DIR/mailserver.sql"

tar -czf "$BACKUP_DIR/maildata.tar.gz" -C /var/mail vhosts
tar -czf "$BACKUP_DIR/config.tar.gz" /etc/postfix /etc/dovecot /etc/rspamd/local.d \
  /etc/unbound/unbound.conf.d /etc/fail2ban/jail.local /etc/patrabahok /var/lib/rspamd/dkim

echo "Backup written to $BACKUP_DIR — copy it off this server."
```

Copy `$BACKUP_DIR` somewhere off the server (another host, object storage, etc.) — a backup
that only exists on the server it's backing up isn't a backup.

## Restore onto a new server

1. Run the installer fresh on the new server (same domain/hostname) up through the database
   phase, then stop before it generates a *new* DKIM key (or just let it, and overwrite
   afterward — see step 4).
2. Restore the database: `mysql --defaults-extra-file=/etc/patrabahok/mysql-admin.cnf mailserver < mailserver.sql`
3. Restore mail data: `tar -xzf maildata.tar.gz -C /var/mail && chown -R vmail:vmail /var/mail/vhosts`
4. Restore DKIM keys to `/var/lib/rspamd/dkim/` (so the DNS TXT record you already published
   still matches), `chown _rspamd:_rspamd`, then `systemctl restart rspamd`.
5. Restore `/etc/patrabahok/secrets.env` and `mysql-admin.cnf` **only if** the new server's
   generated database passwords weren't already applied — otherwise leave the freshly generated
   ones in place and just make sure the database dump you restored matches.
6. Point DNS (A record) at the new server's IP.
7. Run `patrabahok status` and `/opt/patrabahok/current/bin/patrabahok-installer verify`.

## Mailbox-to-mailbox migration (e.g. from another mail server)

Use `doveadm sync` or `imapsync` against the new server's IMAPS endpoint once a mailbox exists
(`patrabahok mailbox add`). Both tools operate over standard IMAP and don't need shell access to
this server.
