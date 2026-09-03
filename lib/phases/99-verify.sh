#!/usr/bin/env bash
set -euo pipefail
PATRABAHOK_HOME="${PATRABAHOK_HOME:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
# shellcheck source=../core/log.sh
. "$PATRABAHOK_HOME/lib/core/log.sh"
# shellcheck source=../core/state.sh
. "$PATRABAHOK_HOME/lib/core/state.sh"
state_init

phase_run() {
  local ok=1

  log_info "Checking postfix configuration..."
  if postfix check 2>&1; then log_ok "postfix check passed"; else log_error "postfix check failed"; ok=0; fi

  log_info "Checking dovecot configuration..."
  if doveconf -n >/dev/null 2>&1; then log_ok "doveconf OK"; else log_error "doveconf failed"; ok=0; fi

  log_info "Checking rspamd configuration..."
  if rspamadm configtest 2>&1; then log_ok "rspamd config OK"; else log_error "rspamd configtest failed"; ok=0; fi

  local mail_hostname domain tls_cert_dir
  mail_hostname="$(state_get hostname)"
  domain="$(state_get domain)"
  tls_cert_dir="$(state_get tls_cert_dir)"

  if [ -n "$tls_cert_dir" ] && [ -f "${tls_cert_dir}/fullchain.pem" ]; then
    log_info "Checking TLS certificate..."
    if openssl x509 -in "${tls_cert_dir}/fullchain.pem" -noout -checkend 0 >/dev/null 2>&1; then
      log_ok "TLS certificate is valid and not expired."
    else
      log_error "TLS certificate missing or expired."
      ok=0
    fi
  fi

  local svc
  for svc in postfix dovecot rspamd redis-server clamav-daemon fail2ban unbound mariadb; do
    if systemctl is-active --quiet "$svc"; then
      log_ok "service active: $svc"
    else
      log_error "service NOT active: $svc"
      ok=0
    fi
  done

  if [ -n "$domain" ] && command -v patrabahok >/dev/null 2>&1; then
    log_info "Running a loopback send/receive test for ${domain}..."
    local testuser="verify-$(date +%s)"
    local testaddr="${testuser}@${domain}"
    local testpass
    testpass="$(openssl rand -base64 18 | tr -dc 'A-Za-z0-9')"

    if patrabahok mailbox add "$testaddr" --password "$testpass" --quota 50M >/dev/null 2>&1; then
      printf 'Subject: patrabahok verify test\n\nThis is an automated self-test message.\n' \
        | sendmail -f "postmaster@${domain}" "$testaddr" || true

      local maildir="/var/mail/vhosts/${domain}/${testuser}/Maildir/new"
      local tries=0 delivered=0
      while [ "$tries" -lt 15 ]; do
        if [ -n "$(find "$maildir" -type f 2>/dev/null)" ]; then
          delivered=1
          break
        fi
        tries=$((tries + 1))
        sleep 1
      done

      if [ "$delivered" -eq 1 ]; then
        log_ok "Loopback delivery test passed (message reached ${testaddr})."
      else
        log_error "Loopback delivery test FAILED — message never arrived in ${maildir}. Check /var/log/mail.log."
        ok=0
      fi

      patrabahok mailbox remove "$testaddr" --force >/dev/null 2>&1 || true
    else
      log_warn "Could not create a temporary test mailbox — skipping loopback test."
    fi
  fi

  if [ "$ok" -eq 1 ]; then
    log_ok "All checks passed."
    return 0
  fi
  log_error "One or more checks failed — review the output above and /var/log/patrabahok/install.log."
  return 1
}

phase_run
