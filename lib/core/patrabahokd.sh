#!/usr/bin/env bash
# Helpers for patrabahokd's systemd unit. Requires log.sh, state.sh and template.sh.

# render_patrabahokd_unit: writes patrabahokd's systemd unit from installer state. 95-cli
# calls it on install; 90-webmail calls it again when the webmail choice changes the
# upstream that the public :443 listener proxies to.
render_patrabahokd_unit() {
  local tls_cert_dir
  tls_cert_dir="$(state_get tls_cert_dir)"
  render_template "$PATRABAHOK_HOME/templates/systemd/patrabahokd.service.tmpl" /etc/systemd/system/patrabahokd.service \
    "TLS_CERT=${tls_cert_dir}/fullchain.pem" "TLS_KEY=${tls_cert_dir}/privkey.pem" \
    "WEBMAIL_UPSTREAM=$(state_get webmail_upstream)"
}

# ensure_current_patrabahokd: on an upgrade, the installed binary may predate flags the
# unit now passes (and features Postfix now depends on, like the policy service). If it
# does, rebuild it, which also re-renders and restarts the unit. Returns 1 when there is
# no installed patrabahokd yet (a fresh install, where 95-cli runs later anyway), 0
# otherwise.
ensure_current_patrabahokd() {
  [ -f /etc/systemd/system/patrabahokd.service ] && [ -x /usr/local/bin/patrabahokd ] || return 1
  local flag
  for flag in -webmail-upstream -policy-addr; do
    if ! /usr/local/bin/patrabahokd -h 2>&1 | grep -q -- "$flag"; then
      log_info "Rebuilding patrabahokd (the installed binary predates ${flag})..."
      bash "$PATRABAHOK_HOME/lib/phases/95-cli.sh" || die "Rebuilding patrabahokd failed."
      return 0
    fi
  done
  return 0
}
