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

  local domain tls_cert_dir
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

  local svc webmail_services=()
  case "$(state_get webmail)" in
    roundcube) webmail_services=(nginx "php$(php -r 'echo PHP_MAJOR_VERSION.".".PHP_MINOR_VERSION;' 2>/dev/null)-fpm") ;;
    custom) webmail_services=(postgresql patrabahok-mail) ;;
  esac
  for svc in postfix dovecot rspamd redis-server clamav-daemon fail2ban unbound mariadb patrabahokd "${webmail_services[@]}"; do
    if systemctl is-active --quiet "$svc"; then
      log_ok "service active: $svc"
    else
      log_error "service NOT active: $svc"
      ok=0
    fi
  done

  if [ -S /run/patrabahok/api.sock ]; then
    if curl -fsS --unix-socket /run/patrabahok/api.sock http://localhost/healthz >/dev/null 2>&1; then
      log_ok "patrabahokd API responding on /run/patrabahok/api.sock"
    else
      log_error "patrabahokd API socket exists but did not respond to /healthz"
      ok=0
    fi
  fi

  # Postfix consults this for every recipient; if it's down, group mail is deferred.
  if timeout 3 bash -c 'exec 3<>/dev/tcp/127.0.0.1/10040' 2>/dev/null; then
    log_ok "Group policy service listening on 127.0.0.1:10040"
  else
    log_error "Group policy service is not listening on 127.0.0.1:10040 (check 'journalctl -u patrabahokd')"
    ok=0
  fi

  local mail_hostname code
  mail_hostname="$(state_get hostname)"
  if [ -n "$mail_hostname" ] && [ -n "$domain" ]; then
    # Over loopback but under the real hostname, so SNI and certificate verification
    # behave exactly as they do for a client on the internet.
    code="$(curl -s -o /dev/null -w '%{http_code}' --resolve "${mail_hostname}:443:127.0.0.1" \
      "https://${mail_hostname}/api/${domain}/mailbox" 2>/dev/null || true)"
    if [ "$code" = "401" ]; then
      log_ok "Public API responding at https://${mail_hostname}/api/${domain}/ (rejects requests without a key)"
    else
      log_error "Public API at https://${mail_hostname}/api/${domain}/mailbox returned HTTP ${code:-<no response>} (expected 401 without a key)"
      ok=0
    fi

    if [ -n "$(state_get webmail_upstream)" ]; then
      code="$(curl -s -o /dev/null -w '%{http_code}' --resolve "${mail_hostname}:443:127.0.0.1" \
        "https://${mail_hostname}/" 2>/dev/null || true)"
      case "$code" in
        2??|3??) log_ok "Webmail ($(state_get webmail)) responding at https://${mail_hostname}/" ;;
        *) log_error "Webmail at https://${mail_hostname}/ returned HTTP ${code:-<no response>}"; ok=0 ;;
      esac
    fi
  fi

  if [ -n "$domain" ] && command -v patrabahok >/dev/null 2>&1; then
    log_info "Running a loopback send/receive test for ${domain}..."
    local testuser
    testuser="verify-$(date +%s)"
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
