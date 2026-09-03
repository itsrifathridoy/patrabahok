#!/usr/bin/env bash
set -euo pipefail
PATRABAHOK_HOME="${PATRABAHOK_HOME:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
# shellcheck source=../core/log.sh
. "$PATRABAHOK_HOME/lib/core/log.sh"
# shellcheck source=../core/os.sh
. "$PATRABAHOK_HOME/lib/core/os.sh"
# shellcheck source=../core/state.sh
. "$PATRABAHOK_HOME/lib/core/state.sh"
state_init

PACKAGES=(
  postfix postfix-pcre postfix-mysql
  dovecot-core dovecot-imapd dovecot-lmtpd dovecot-mysql
  mariadb-server mariadb-client
  rspamd redis-server
  clamav clamav-daemon clamav-freshclam
  certbot
  unbound
  ufw fail2ban
  jq dnsutils
  openssl uuid-runtime
)

phase_run() {
  log_info "Updating package lists..."
  apt_update

  log_info "Pre-seeding postfix so its Debconf install stays non-interactive..."
  local domain
  domain="$(state_get domain 2>/dev/null || true)"
  [ -z "$domain" ] && domain="localhost"
  debconf-set-selections <<EOF
postfix postfix/main_mailer_type select Internet Site
postfix postfix/mailname string ${domain}
EOF

  log_info "Installing packages (this can take a few minutes)..."
  apt_install "${PACKAGES[@]}"

  log_info "Stopping services that phases below will (re)configure before enabling them..."
  systemctl stop postfix dovecot rspamd 2>/dev/null || true

  log_info "Disabling and masking sendmail/exim if present (package conflicts)..."
  for alt in exim4 sendmail; do
    systemctl disable --now "$alt" >/dev/null 2>&1 || true
  done

  return 0
}

phase_run
