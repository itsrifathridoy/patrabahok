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

RSPAMD_USER="_rspamd"

phase_run() {
  log_info "Starting Redis (Rspamd Bayes/ratelimit state)..."
  systemctl enable --now redis-server >/dev/null 2>&1
  systemctl restart redis-server

  log_info "Updating ClamAV virus database (this can take a minute on first run)..."
  systemctl stop clamav-freshclam 2>/dev/null || true
  timeout 300 freshclam --quiet || log_warn "freshclam did not finish cleanly — clamd may start with a stale/partial database; it will keep retrying via the freshclam service."
  systemctl enable --now clamav-freshclam >/dev/null 2>&1
  systemctl enable --now clamav-daemon >/dev/null 2>&1
  systemctl restart clamav-daemon

  local tries=0
  until [ -S /var/run/clamav/clamd.ctl ]; do
    tries=$((tries + 1))
    [ "$tries" -gt 60 ] && { log_warn "clamd socket did not appear in time; continuing anyway."; break; }
    sleep 1
  done

  if id "$RSPAMD_USER" >/dev/null 2>&1 && getent group clamav >/dev/null 2>&1; then
    usermod -aG clamav "$RSPAMD_USER"
  fi

  log_info "Configuring Rspamd (spam scoring, DKIM sign/verify, DMARC, ClamAV)..."
  mkdir -p /etc/rspamd/local.d /var/lib/rspamd/dkim
  chown "${RSPAMD_USER}:${RSPAMD_USER}" /var/lib/rspamd/dkim
  chmod 750 /var/lib/rspamd/dkim

  local f base
  for f in "$PATRABAHOK_HOME"/templates/rspamd/local.d/*.tmpl; do
    base="$(basename "$f" .tmpl)"
    render_template "$f" "/etc/rspamd/local.d/${base}"
  done

  rspamadm configtest || die "Rspamd configuration test failed — see output above."

  systemctl enable rspamd >/dev/null 2>&1
  systemctl restart rspamd

  local tries2=0
  until [ -S /run/rspamd/rspamd-milter.sock ]; do
    tries2=$((tries2 + 1))
    [ "$tries2" -gt 30 ] && die "Rspamd milter socket did not appear at /run/rspamd/rspamd-milter.sock — check 'journalctl -u rspamd'."
    sleep 1
  done

  systemctl restart postfix 2>/dev/null || true

  log_ok "Rspamd + ClamAV running; Postfix is now filtering mail through the Rspamd milter."
  return 0
}

phase_run
