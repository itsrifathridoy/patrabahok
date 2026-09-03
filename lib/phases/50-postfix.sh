#!/usr/bin/env bash
set -euo pipefail
PATRABAHOK_HOME="${PATRABAHOK_HOME:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
# shellcheck source=../core/log.sh
. "$PATRABAHOK_HOME/lib/core/log.sh"
# shellcheck source=../core/state.sh
. "$PATRABAHOK_HOME/lib/core/state.sh"
# shellcheck source=../core/secrets.sh
. "$PATRABAHOK_HOME/lib/core/secrets.sh"
# shellcheck source=../core/template.sh
. "$PATRABAHOK_HOME/lib/core/template.sh"
state_init
secrets_load

VMAIL_UID=5000
VMAIL_GID=5000
VMAIL_HOME=/var/mail/vhosts

setup_vmail_user() {
  getent group vmail >/dev/null 2>&1 || groupadd -g "$VMAIL_GID" vmail
  id vmail >/dev/null 2>&1 || useradd -m -d "$VMAIL_HOME" -g vmail -u "$VMAIL_UID" -s /usr/sbin/nologin vmail
  mkdir -p "$VMAIL_HOME"
  chown -R vmail:vmail "$VMAIL_HOME"
  chmod 750 "$VMAIL_HOME"
}

setup_unbound() {
  log_info "Configuring local recursive resolver (unbound) for DNSBL/Rspamd lookups..."
  render_template "$PATRABAHOK_HOME/templates/unbound/patrabahok.conf.tmpl" \
    /etc/unbound/unbound.conf.d/patrabahok.conf

  if [ ! -f /var/lib/unbound/root.key ]; then
    mkdir -p /var/lib/unbound
    # unbound-anchor exits 1 when it creates/updates the anchor (not an error) and 0
    # when nothing needed to change — either way, only the resulting file matters.
    unbound-anchor -a /var/lib/unbound/root.key || true
    chown unbound:unbound /var/lib/unbound/root.key 2>/dev/null || true
  fi

  if [ -f /etc/systemd/resolved.conf ]; then
    if ! grep -q '^DNSStubListener=no' /etc/systemd/resolved.conf 2>/dev/null; then
      sed -i 's/^#\?DNSStubListener=.*/DNSStubListener=no/' /etc/systemd/resolved.conf
      grep -q '^DNSStubListener=no' /etc/systemd/resolved.conf || echo 'DNSStubListener=no' >> /etc/systemd/resolved.conf
      systemctl restart systemd-resolved 2>/dev/null || true
    fi
  fi

  systemctl enable --now unbound >/dev/null 2>&1
  systemctl restart unbound

  chattr -i /etc/resolv.conf 2>/dev/null || true
  if [ -L /etc/resolv.conf ] || [ -f /etc/resolv.conf ]; then
    rm -f /etc/resolv.conf
  fi
  {
    echo "nameserver 127.0.0.1"
    echo "nameserver 1.1.1.1"
  } > /etc/resolv.conf

  sleep 1
  if ! dig +short @127.0.0.1 example.com >/dev/null 2>&1; then
    log_warn "Local resolver (unbound) does not appear to be answering queries yet — DNSBL checks may be degraded."
  else
    log_ok "Local resolver is answering queries."
  fi
}

configure_master_cf() {
  log_info "Configuring master.cf services (postscreen, submission, smtps)..."

  postconf -M smtp/inet="smtp inet n - y - 1 postscreen"
  postconf -M smtpd/pass="smtpd pass - - y - - smtpd"
  postconf -M dnsblog/unix="dnsblog unix - - y - 0 dnsblog"
  postconf -M tlsproxy/unix="tlsproxy unix - - y - 0 tlsproxy"

  postconf -M submission/inet="submission inet n - y - - smtpd"
  postconf -P "submission/inet/syslog_name=postfix/submission"
  postconf -P "submission/inet/smtpd_tls_security_level=encrypt"
  postconf -P "submission/inet/smtpd_sasl_auth_enable=yes"
  postconf -P "submission/inet/smtpd_reject_unlisted_recipient=yes"
  postconf -P "submission/inet/smtpd_recipient_restrictions=permit_sasl_authenticated,reject"
  postconf -P "submission/inet/smtpd_relay_restrictions=permit_sasl_authenticated,reject"
  postconf -P "submission/inet/milter_macro_daemon_name=ORIGINATING"

  postconf -M smtps/inet="smtps inet n - y - - smtpd"
  postconf -P "smtps/inet/syslog_name=postfix/smtps"
  postconf -P "smtps/inet/smtpd_tls_wrappermode=yes"
  postconf -P "smtps/inet/smtpd_sasl_auth_enable=yes"
  postconf -P "smtps/inet/smtpd_reject_unlisted_recipient=yes"
  postconf -P "smtps/inet/smtpd_recipient_restrictions=permit_sasl_authenticated,reject"
  postconf -P "smtps/inet/smtpd_relay_restrictions=permit_sasl_authenticated,reject"
  postconf -P "smtps/inet/milter_macro_daemon_name=ORIGINATING"
}

phase_run() {
  local mail_hostname domain tls_cert_dir db_name db_readonly_user
  mail_hostname="$(state_get hostname)"
  domain="$(state_get domain)"
  tls_cert_dir="$(state_get tls_cert_dir)"
  db_name="$(state_get db_name)"
  db_readonly_user="$(state_get db_readonly_user)"

  [ -n "$mail_hostname" ] && [ -n "$domain" ] && [ -n "$tls_cert_dir" ] && [ -n "$db_name" ] \
    || die "Missing state (hostname/domain/tls_cert_dir/db_name) — run earlier phases first."

  setup_vmail_user
  setup_unbound

  render_template "$PATRABAHOK_HOME/templates/postfix/main.cf.tmpl" /etc/postfix/main.cf \
    "MAIL_HOSTNAME=${mail_hostname}" \
    "DOMAIN=${domain}" \
    "TLS_CERT=${tls_cert_dir}/fullchain.pem" \
    "TLS_KEY=${tls_cert_dir}/privkey.pem"

  render_template "$PATRABAHOK_HOME/templates/postfix/mysql-virtual-mailbox-domains.cf.tmpl" \
    /etc/postfix/mysql-virtual-mailbox-domains.cf \
    "DB_READONLY_USER=${db_readonly_user}" "DB_READONLY_PASSWORD=${MAIL_DB_READONLY_PASSWORD}" "DB_NAME=${db_name}"

  render_template "$PATRABAHOK_HOME/templates/postfix/mysql-virtual-mailbox-maps.cf.tmpl" \
    /etc/postfix/mysql-virtual-mailbox-maps.cf \
    "DB_READONLY_USER=${db_readonly_user}" "DB_READONLY_PASSWORD=${MAIL_DB_READONLY_PASSWORD}" "DB_NAME=${db_name}"

  render_template "$PATRABAHOK_HOME/templates/postfix/mysql-virtual-alias-maps.cf.tmpl" \
    /etc/postfix/mysql-virtual-alias-maps.cf \
    "DB_READONLY_USER=${db_readonly_user}" "DB_READONLY_PASSWORD=${MAIL_DB_READONLY_PASSWORD}" "DB_NAME=${db_name}"

  chown root:postfix /etc/postfix/mysql-virtual-*.cf
  chmod 640 /etc/postfix/mysql-virtual-*.cf

  configure_master_cf

  [ -f /etc/aliases ] || echo "postmaster: root" > /etc/aliases
  newaliases 2>/dev/null || true

  # Chrooted Postfix services (see master.cf's chroot column) read DNS config from a copy
  # inside the queue directory, not /etc/resolv.conf directly — without this, postscreen's
  # DNSBL lookups silently bypass the local unbound resolver we just set up.
  mkdir -p /var/spool/postfix/etc
  cp -f /etc/resolv.conf /var/spool/postfix/etc/resolv.conf
  cp -f /etc/services /var/spool/postfix/etc/services 2>/dev/null || true

  postfix check || die "postfix check failed — see output above."

  systemctl enable postfix >/dev/null 2>&1
  systemctl restart postfix

  log_ok "Postfix configured and running."
  return 0
}

phase_run
