#!/usr/bin/env bash
set -euo pipefail
PATRABAHOK_HOME="${PATRABAHOK_HOME:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
# shellcheck source=../core/log.sh
. "$PATRABAHOK_HOME/lib/core/log.sh"
# shellcheck source=../core/state.sh
. "$PATRABAHOK_HOME/lib/core/state.sh"
# shellcheck source=../core/prompt.sh
. "$PATRABAHOK_HOME/lib/core/prompt.sh"

state_init

is_valid_domain() {
  [[ "$1" =~ ^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$ ]]
}

is_valid_email() {
  [[ "$1" =~ ^[^[:space:]@]+@[^[:space:]@]+\.[^[:space:]@]+$ ]]
}

ensure_dig() {
  command -v dig >/dev/null 2>&1 && return 0
  log_info "Installing dnsutils (needed for DNS checks)..."
  apt-get update -y >/dev/null 2>&1 || true
  DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends dnsutils >/dev/null 2>&1 \
    || die "Could not install dnsutils (dig)."
}

detect_public_ip() {
  local ip
  ip=$(curl -fsSL --max-time 5 https://api.ipify.org 2>/dev/null || true)
  if [ -z "$ip" ]; then
    ip=$(curl -fsSL --max-time 5 https://ifconfig.me 2>/dev/null || true)
  fi
  printf '%s' "$ip"
}

check_dns_record() {
  local label="$1" query="$2" expect_substring="$3"
  local result
  result=$(dig +short "$query" @1.1.1.1 2>/dev/null || true)
  if [ -n "$result" ] && printf '%s' "$result" | grep -qi -- "$expect_substring"; then
    log_ok "$label: OK ($result)"
    return 0
  fi
  log_warn "$label: not confirmed yet (got: '${result:-<empty>}', expected something containing '$expect_substring')"
  return 1
}

phase_run() {
  echo
  log_info "patrabahok installer — mail server setup"
  echo

  ask DOMAIN "Primary mail domain (e.g. example.com)" ""
  is_valid_domain "$DOMAIN" || die "Invalid domain: $DOMAIN"

  ask MAIL_HOSTNAME "Mail server hostname (FQDN)" "mail.${DOMAIN}"
  is_valid_domain "$MAIL_HOSTNAME" || die "Invalid hostname: $MAIL_HOSTNAME"

  ask ADMIN_EMAIL "Admin email (used for postmaster, TLS renewal, DMARC reports)" "postmaster@${DOMAIN}"
  is_valid_email "$ADMIN_EMAIL" || die "Invalid email: $ADMIN_EMAIL"

  ask_finalize

  state_set domain "$DOMAIN"
  state_set hostname "$MAIL_HOSTNAME"
  state_set admin_email "$ADMIN_EMAIL"
  state_add_to_list domains "$DOMAIN"

  ensure_dig

  log_info "Detecting this server's public IP..."
  local server_ip
  server_ip=$(detect_public_ip)
  if [ -z "$server_ip" ]; then
    log_warn "Could not auto-detect the public IP. Skipping DNS pre-checks (will still work, but verify DNS manually)."
  else
    log_info "Public IP: $server_ip"
    state_set server_ip "$server_ip"

    local a_ok=0 mx_ok=0
    check_dns_record "A record for $MAIL_HOSTNAME" "A $MAIL_HOSTNAME" "$server_ip" && a_ok=1 || true
    check_dns_record "MX record for $DOMAIN" "MX $DOMAIN" "$MAIL_HOSTNAME" && mx_ok=1 || true

    if [ "$a_ok" -eq 0 ] || [ "$mx_ok" -eq 0 ]; then
      log_warn "DNS is not fully set up yet. You'll need:"
      [ "$a_ok" -eq 0 ] && log_warn "  A record: $MAIL_HOSTNAME -> $server_ip"
      [ "$mx_ok" -eq 0 ] && log_warn "  MX record: $DOMAIN -> $MAIL_HOSTNAME (priority 10)"
      if [ "$PATRABAHOK_NON_INTERACTIVE" != "1" ]; then
        while ! confirm "Continue anyway? (DNS can be fixed later; TLS issuance in a later step will fail until the A record resolves)" "n"; do
          log_info "Re-checking DNS..."
          check_dns_record "A record for $MAIL_HOSTNAME" "A $MAIL_HOSTNAME" "$server_ip" && a_ok=1 || a_ok=0
          check_dns_record "MX record for $DOMAIN" "MX $DOMAIN" "$MAIL_HOSTNAME" && mx_ok=1 || mx_ok=0
          [ "$a_ok" -eq 1 ] && [ "$mx_ok" -eq 1 ] && break
          confirm "DNS still not confirmed. Try again?" "y" || die "Aborted — fix DNS and re-run the installer."
        done
      else
        log_warn "Non-interactive mode: continuing despite unconfirmed DNS. TLS issuance may fail later."
      fi
    fi
  fi

  log_info "Checking outbound port 25 (many VPS providers block this by default)..."
  if timeout 6 bash -c "exec 3<>/dev/tcp/aspmx.l.google.com/25" 2>/dev/null; then
    exec 3>&- 3<&- 2>/dev/null || true
    log_ok "Outbound port 25 appears open."
  else
    log_warn "Could not open an outbound connection on port 25."
    log_warn "Many cloud providers block this by default for new accounts — you may need to"
    log_warn "request it be unblocked (support ticket) before outbound mail delivery will work."
  fi

  return 0
}

phase_run
