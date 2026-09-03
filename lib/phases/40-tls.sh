#!/usr/bin/env bash
set -euo pipefail
PATRABAHOK_HOME="${PATRABAHOK_HOME:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
# shellcheck source=../core/log.sh
. "$PATRABAHOK_HOME/lib/core/log.sh"
# shellcheck source=../core/state.sh
. "$PATRABAHOK_HOME/lib/core/state.sh"
state_init

phase_run() {
  local mail_hostname admin_email
  mail_hostname="$(state_get hostname)"
  admin_email="$(state_get admin_email)"
  [ -n "$mail_hostname" ] && [ -n "$admin_email" ] || die "hostname/admin_email missing from state — run preflight first."

  log_info "Requesting a Let's Encrypt certificate for ${mail_hostname} (standalone, port 80)..."
  systemctl stop nginx apache2 2>/dev/null || true

  certbot certonly --standalone --non-interactive --agree-tos \
    -m "$admin_email" \
    -d "$mail_hostname" \
    --cert-name "$mail_hostname" \
    --keep-until-expiring \
    || die "Certbot failed. Ensure ${mail_hostname} resolves to this server's public IP and port 80 is reachable from the internet, then re-run."

  local cert_dir="/etc/letsencrypt/live/${mail_hostname}"
  [ -f "${cert_dir}/fullchain.pem" ] || die "Certificate files not found at ${cert_dir}"
  state_set tls_cert_dir "$cert_dir"

  log_info "Installing renewal deploy hook..."
  mkdir -p /etc/letsencrypt/renewal-hooks/deploy
  cat > /etc/letsencrypt/renewal-hooks/deploy/patrabahok-reload.sh <<'EOF'
#!/bin/sh
systemctl reload postfix 2>/dev/null || true
systemctl reload dovecot 2>/dev/null || true
EOF
  chmod +x /etc/letsencrypt/renewal-hooks/deploy/patrabahok-reload.sh

  systemctl enable --now certbot.timer >/dev/null 2>&1 || true

  log_info "Testing renewal (dry run)..."
  # --no-random-sleep-on-renew: certbot's renew subcommand otherwise sleeps for a random
  # delay of several minutes (thundering-herd protection for scheduled cron/timer renewals),
  # which we don't want blocking an interactive/scripted install.
  certbot renew --dry-run --no-random-sleep-on-renew --cert-name "$mail_hostname" >/dev/null \
    || log_warn "Renewal dry-run failed — check 'certbot certificates' and 'journalctl -u certbot' later."

  log_ok "TLS certificate ready at ${cert_dir}"
  return 0
}

phase_run
