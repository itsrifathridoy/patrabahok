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

RSPAMD_USER="$(state_get rspamd_user)"
[ -n "$RSPAMD_USER" ] || RSPAMD_USER="$(detect_rspamd_user)"
RSPAMD_GROUP="$(state_get rspamd_group)"
[ -n "$RSPAMD_GROUP" ] || RSPAMD_GROUP="$(id -gn "$RSPAMD_USER" 2>/dev/null || printf '%s' "$RSPAMD_USER")"
SELECTOR="mail"
DKIM_DIR="/var/lib/rspamd/dkim"
# Not /root: patrabahokd's systemd sandbox (ProtectHome=read-only) can't write there,
# and domains added later (CLI/API/dashboard) need to regenerate this file too — see
# cli/internal/mailbox/dkim_provision.go, which writes here as well.
DNS_DUMP_DIR="/var/lib/patrabahok/dns-records"

# normalize_dkim_record_name DOMAIN — rspamadm dkim_keygen emits a bare, zone-file-
# relative name ("mail._domainkey"), only meaningful inside a zone file that already has
# $ORIGIN set to this exact domain. Rewrites the record file to start with the fully
# qualified name instead, so it's safe to paste as-is into a DNS provider's "Name"
# field. Runs every time (not just after a fresh generation), so a file written before
# this fix existed gets self-healed the next time this phase runs.
normalize_dkim_record_name() {
  local domain="$1"
  local record_path="${DKIM_DIR}/${domain}.${SELECTOR}.txt"
  [ -f "$record_path" ] || return 0
  head -n1 "$record_path" | grep -qF "${SELECTOR}._domainkey.${domain}" && return 0
  sed -i "1s/^${SELECTOR}\._domainkey\b/${SELECTOR}._domainkey.${domain}/" "$record_path"
}

# generate_dkim_key DOMAIN — idempotent: generates a key+DNS-record pair only if one
# doesn't already exist for this domain/selector.
generate_dkim_key() {
  local domain="$1"
  local key_path="${DKIM_DIR}/${domain}.${SELECTOR}.key"
  local record_path="${DKIM_DIR}/${domain}.${SELECTOR}.txt"

  if [ -f "$key_path" ]; then
    log_info "DKIM key for ${domain} already exists, reusing it."
    normalize_dkim_record_name "$domain"
    return 0
  fi

  log_info "Generating DKIM key for ${domain} (selector: ${SELECTOR})..."
  rspamadm dkim_keygen -s "$SELECTOR" -d "$domain" -k "$key_path" > "$record_path"
  chown "${RSPAMD_USER}:${RSPAMD_GROUP}" "$key_path"
  chmod 640 "$key_path"
  chmod 644 "$record_path"
  normalize_dkim_record_name "$domain"
}

write_dns_records_file() {
  local domain="$1" mail_hostname="$2" server_ip="$3" admin_email="$4"
  local record_path="${DKIM_DIR}/${domain}.${SELECTOR}.txt"
  local out="${DNS_DUMP_DIR}/patrabahok-dns-${domain}.txt"
  mkdir -p "$DNS_DUMP_DIR"
  chmod 700 "$DNS_DUMP_DIR"

  {
    echo "DNS records required for ${domain} (mail server: ${mail_hostname})"
    echo "======================================================================"
    echo
    echo "-- A record (only needed once, even with multiple domains) --"
    echo "${mail_hostname}.   IN  A      ${server_ip}"
    echo
    echo "-- MX record --"
    echo "${domain}.   IN  MX  10  ${mail_hostname}."
    echo
    echo "-- SPF (TXT) --"
    echo "${domain}.   IN  TXT    \"v=spf1 mx -all\""
    echo
    echo "-- DKIM (TXT) --"
    if [ -f "$record_path" ]; then
      cat "$record_path"
    else
      echo "(DKIM record file not found at ${record_path} — check 'rspamadm dkim_keygen' output manually.)"
    fi
    echo
    echo "-- DMARC (TXT) — start at p=none, monitor, then move to quarantine/reject --"
    echo "_dmarc.${domain}.   IN  TXT    \"v=DMARC1; p=none; rua=mailto:${admin_email}\""
    echo
    echo "-- MTA-STS (TXT) — optional, requires you to host a policy file yourself; --"
    echo "-- this installer does not set up that hosting (see docs/ROADMAP.md).     --"
    echo "_mta-sts.${domain}.   IN  TXT    \"v=STSv1; id=$(date -u +%Y%m%d%H%M%S)\""
    echo
  } > "$out"
  chmod 600 "$out"
  printf '%s' "$out"
}

phase_run() {
  local mail_hostname server_ip admin_email
  mail_hostname="$(state_get hostname)"
  server_ip="$(state_get server_ip "<this-server-public-ip>")"
  admin_email="$(state_get admin_email)"

  mkdir -p "$DKIM_DIR"

  local domain out_file
  while IFS= read -r domain; do
    [ -z "$domain" ] && continue
    [ -z "$admin_email" ] && admin_email="postmaster@${domain}"

    generate_dkim_key "$domain"
    out_file="$(write_dns_records_file "$domain" "$mail_hostname" "$server_ip" "$admin_email")"

    echo
    log_ok "DNS records for ${domain} written to ${out_file}:"
    echo
    cat "$out_file"
    echo
  done < <(state_get_list domains)

  systemctl restart rspamd 2>/dev/null || true

  log_warn "Add the DNS records above before sending real mail. DKIM signing and DMARC"
  log_warn "verification only take effect once those DNS records propagate."
  return 0
}

phase_run
