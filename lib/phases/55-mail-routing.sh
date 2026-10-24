#!/usr/bin/env bash
# Groups, forwarding and catch-all routing (lib/core/mailrouting.sh). On a fresh install
# 50-postfix has already applied this and the phase is a cheap re-apply. It exists for
# servers installed before groups: there 50-postfix is already done and would never
# re-run.
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
# shellcheck source=../core/mailrouting.sh
. "$PATRABAHOK_HOME/lib/core/mailrouting.sh"
# shellcheck source=../core/patrabahokd.sh
. "$PATRABAHOK_HOME/lib/core/patrabahokd.sh"
state_init
secrets_load

phase_run() {
  # Postfix must not start consulting the policy service before something answers on
  # it, or every message to a group would be deferred until the next rebuild. On an
  # upgrade, bring patrabahokd up to date (and running with -policy-addr) first.
  if ensure_current_patrabahokd; then
    render_patrabahokd_unit
    systemctl daemon-reload
    systemctl restart patrabahokd
  fi

  log_info "Configuring group, forwarding and catch-all routing in Postfix..."
  apply_mail_routing
  postfix check >/dev/null 2>&1 || { postfix check; die "postfix check failed after applying mail routing."; }
  systemctl reload postfix
  log_ok "Mail routing ready: groups (with posting policies), forwarding, and catch-all addresses."
  return 0
}

phase_run
