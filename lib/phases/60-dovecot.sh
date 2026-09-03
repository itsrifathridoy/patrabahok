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

phase_run() {
  local tls_cert_dir db_name db_readonly_user
  tls_cert_dir="$(state_get tls_cert_dir)"
  db_name="$(state_get db_name)"
  db_readonly_user="$(state_get db_readonly_user)"
  [ -n "$tls_cert_dir" ] && [ -n "$db_name" ] || die "Missing state (tls_cert_dir/db_name) — run earlier phases first."

  render_template "$PATRABAHOK_HOME/templates/dovecot/dovecot-sql.conf.ext.tmpl" \
    /etc/dovecot/dovecot-sql.conf.ext \
    "DB_NAME=${db_name}" "DB_READONLY_USER=${db_readonly_user}" "DB_READONLY_PASSWORD=${MAIL_DB_READONLY_PASSWORD}"
  chown root:dovecot /etc/dovecot/dovecot-sql.conf.ext
  chmod 640 /etc/dovecot/dovecot-sql.conf.ext

  render_template "$PATRABAHOK_HOME/templates/dovecot/99-patrabahok.conf.tmpl" \
    /etc/dovecot/conf.d/99-patrabahok.conf \
    "TLS_CERT=${tls_cert_dir}/fullchain.pem" "TLS_KEY=${tls_cert_dir}/privkey.pem"

  if [ -f /etc/dovecot/conf.d/10-auth.conf ]; then
    sed -i 's/^!include auth-system\.conf\.ext/#!include auth-system.conf.ext/' /etc/dovecot/conf.d/10-auth.conf
  fi

  mkdir -p /var/spool/postfix/private
  chown postfix:postfix /var/spool/postfix/private

  doveconf -n >/dev/null || die "doveconf reported a configuration error — see output above."

  systemctl enable dovecot >/dev/null 2>&1
  systemctl restart dovecot

  systemctl restart postfix 2>/dev/null || true

  log_ok "Dovecot configured and running (IMAPS only, SQL auth, LMTP delivery)."
  return 0
}

phase_run
