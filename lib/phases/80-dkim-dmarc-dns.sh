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
# Mirrors cli/internal/mtasts.PolicyDir — patrabahokd's MTA-STS listener serves whatever
# policy file it finds here.
MTA_STS_DIR="/var/lib/patrabahok/mta-sts"

# mta_sts_policy_content MAIL_HOSTNAME — must match mtasts.PolicyContent exactly (Go),
# since the printed DNS TXT record's "id" is a hash of this text and both sides need to
# agree on it.
mta_sts_policy_content() {
  printf 'version: STSv1\nmode: testing\nmx: %s\nmax_age: 604800\n' "$1"
}

# mta_sts_policy_id CONTENT — must match cli/internal/mtasts.PolicyID exactly: first 16
# hex chars of the content's SHA-256, so the DNS record only needs to change when the
# policy actually does.
mta_sts_policy_id() {
  printf '%s' "$1" | sha256sum | cut -c1-16
}

# write_mta_sts_policy DOMAIN MAIL_HOSTNAME — writes the (not-yet-hosted) policy file so
# it's ready the moment 'patrabahok mta-sts enable' issues a certificate for it; mirrors
# cli/internal/mtasts.WritePolicy.
write_mta_sts_policy() {
  local domain="$1" mail_hostname="$2"
  local dir="${MTA_STS_DIR}/${domain}"
  mkdir -p "$dir"
  chmod 755 "$dir"
  mta_sts_policy_content "$mail_hostname" > "${dir}/mta-sts.txt"
  chmod 644 "${dir}/mta-sts.txt"
}

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

# normalize_dkim_record_quoting RECORD_PATH — rspamadm dkim_keygen always splits the TXT
# value into two quoted zone-file segments ("v=DKIM1; k=rsa;" and "p=...") regardless of
# whether the combined value is anywhere near the DNS wire format's real 255-byte-per-
# segment limit (RFC 1035 §3.3). For today's 1024-bit RSA key the whole value is well
# under 255 bytes, so this collapses it to a single quoted segment — confirmed via a live
# `dig` query to match exactly what Cloudflare's API already stores on the wire, and what
# a DNS provider's single "Value" field expects when copy-pasted by hand, rather than the
# two-segment zone-file text (quotes, embedded line break, and all) a user would
# otherwise paste verbatim into a field that wants one continuous string. Stays correct
# automatically if a larger key ever needs genuinely more than one 255-byte segment: this
# only ever produces the minimum segment count the wire format actually requires. Runs
# every time (not just after a fresh generation), so an already-generated file gets
# self-healed the next time it's touched, same as normalize_dkim_record_name.
normalize_dkim_record_quoting() {
  local record_path="$1"
  [ -f "$record_path" ] || return 0
  local quote_count
  quote_count=$(grep -o '"' "$record_path" | wc -l)
  [ "$quote_count" -le 2 ] && return 0

  local joined prefix value
  joined="$(tr -d '\n\t' < "$record_path")"
  prefix="$(awk -F'"' '{print $1}' <<<"$joined")"
  local suffix
  suffix="$(awk -F'"' '{print $NF}' <<<"$joined")"
  # Even-numbered awk -F'"' fields are the text INSIDE quote pairs (odd fields are outside
  # them, e.g. the whitespace rspamadm leaves between its two segments) — concatenating
  # only the even fields reconstructs the value with nothing extra inserted.
  value="$(awk -F'"' '{v=""; for (i=2; i<NF; i+=2) v = v $i; print v}' <<<"$joined")"
  [ -n "$value" ] || return 0

  local chunks="" remaining="$value" chunk
  while [ -n "$remaining" ]; do
    chunk="${remaining:0:255}"
    remaining="${remaining:255}"
    if [ -z "$chunks" ]; then
      chunks="\"${chunk}\""
    else
      chunks="${chunks} \"${chunk}\""
    fi
  done

  printf '%s%s%s\n' "$prefix" "$chunks" "$suffix" > "$record_path"
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
    normalize_dkim_record_quoting "$record_path"
    return 0
  fi

  log_info "Generating DKIM key for ${domain} (selector: ${SELECTOR})..."
  rspamadm dkim_keygen -s "$SELECTOR" -d "$domain" -k "$key_path" > "$record_path"
  chown "${RSPAMD_USER}:${RSPAMD_GROUP}" "$key_path"
  chmod 640 "$key_path"
  chmod 644 "$record_path"
  normalize_dkim_record_name "$domain"
  normalize_dkim_record_quoting "$record_path"
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
    echo "-- MTA-STS (TXT + A, optional) — add both records, then run --"
    echo "-- 'patrabahok mta-sts enable ${domain}' (or use the dashboard's DNS --"
    echo "-- Analysis page) to actually issue the certificate and start hosting --"
    echo "-- the policy file this record points at.                             --"
    echo "mta-sts.${domain}.   IN  A      ${server_ip}"
    local sts_content sts_id
    sts_content="$(mta_sts_policy_content "$mail_hostname")"
    sts_id="$(mta_sts_policy_id "$sts_content")"
    echo "_mta-sts.${domain}.   IN  TXT    \"v=STSv1; id=${sts_id}\""
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
    write_mta_sts_policy "$domain" "$mail_hostname"
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
