#!/usr/bin/env bash
set -euo pipefail
PATRABAHOK_HOME="${PATRABAHOK_HOME:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
# shellcheck source=../core/log.sh
. "$PATRABAHOK_HOME/lib/core/log.sh"
# shellcheck source=../core/state.sh
. "$PATRABAHOK_HOME/lib/core/state.sh"
# shellcheck source=../core/template.sh
. "$PATRABAHOK_HOME/lib/core/template.sh"
state_init

detect_ssh_port() {
  local port
  port=$(grep -iE '^[[:space:]]*Port[[:space:]]+[0-9]+' /etc/ssh/sshd_config 2>/dev/null | awk '{print $2}' | head -n1)
  printf '%s' "${port:-22}"
}

phase_run() {
  local ssh_port
  ssh_port="$(detect_ssh_port)"

  log_info "Configuring ufw (SSH on ${ssh_port}, SMTP 25/587/465, IMAPS 993, HTTP(S) 80/443)..."
  ufw --force reset >/dev/null
  ufw default deny incoming >/dev/null
  ufw default allow outgoing >/dev/null

  ufw allow "${ssh_port}/tcp" comment 'ssh' >/dev/null
  ufw allow 25/tcp comment 'smtp' >/dev/null
  ufw allow 587/tcp comment 'submission' >/dev/null
  ufw allow 465/tcp comment 'smtps' >/dev/null
  ufw allow 993/tcp comment 'imaps' >/dev/null
  ufw allow 80/tcp comment 'acme-http-01' >/dev/null
  ufw allow 443/tcp comment 'https' >/dev/null

  ufw --force enable >/dev/null
  log_ok "ufw enabled: $(ufw status | head -n1)"

  local admin_email
  admin_email="$(state_get admin_email root@localhost)"
  render_template "$PATRABAHOK_HOME/templates/fail2ban/jail.local.tmpl" /etc/fail2ban/jail.local \
    "ADMIN_EMAIL=${admin_email}"

  systemctl enable --now fail2ban >/dev/null 2>&1
  systemctl restart fail2ban

  sleep 1
  fail2ban-client status >/dev/null || die "fail2ban did not start correctly"
  log_ok "fail2ban active with jails: $(fail2ban-client status | grep 'Jail list' | sed 's/.*://')"

  return 0
}

phase_run
