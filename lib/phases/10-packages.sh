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

# rspamd is installed separately (see ensure_rspamd) since its availability/version varies
# more across Ubuntu 24.04/22.04 and Debian 12 than the rest of this list, which is stable
# across all three targets.
BASE_PACKAGES=(
  ca-certificates gnupg
  postfix postfix-pcre postfix-mysql
  dovecot-core dovecot-imapd dovecot-lmtpd dovecot-mysql dovecot-sieve
  mariadb-server mariadb-client
  redis-server
  clamav clamav-daemon clamav-freshclam
  certbot
  unbound unbound-anchor
  ufw fail2ban
  jq dnsutils
  openssl uuid-runtime
)

# Adds the official Rspamd APT repository and retries, but only if the distro's own
# repositories don't already provide it. Unverified fallback path (best-effort against
# rspamd.com's documented repo layout) — if this ever actually triggers, check
# https://rspamd.com/downloads.html for the current key/repo URLs.
ensure_rspamd() {
  if apt_install rspamd; then
    return 0
  fi

  log_warn "rspamd not available from the default repositories — adding the official Rspamd APT repo..."
  mkdir -p /etc/apt/keyrings
  curl -fsSL https://rspamd.com/apt-stable/gpg.key | gpg --dearmor -o /etc/apt/keyrings/rspamd.gpg \
    || die "Failed to fetch/import the Rspamd repository signing key. Check https://rspamd.com/downloads.html for current instructions."

  local codename="$OS_CODENAME"
  [ -n "$codename" ] && [ "$codename" != "unknown" ] || die "Could not determine OS codename for the Rspamd repository."

  cat > /etc/apt/sources.list.d/rspamd.list <<EOF
deb [signed-by=/etc/apt/keyrings/rspamd.gpg] https://rspamd.com/apt-stable/ ${codename} main
deb-src [signed-by=/etc/apt/keyrings/rspamd.gpg] https://rspamd.com/apt-stable/ ${codename} main
EOF

  apt_update
  apt_install rspamd || die "rspamd installation failed even after adding the official repository. Check network access to rspamd.com."
}

phase_run() {
  log_info "Updating package lists..."
  apt_update

  log_info "Pre-seeding postfix so its Debconf install stays non-interactive..."
  local domain
  domain="$(state_get domain 2>/dev/null || true)"
  [ -z "$domain" ] && domain="localhost"
  debconf-set-selections <<EOF
postfix postfix/main_mailer_type select Internet Site
postfix postfix/mailname string ${domain}
EOF

  log_info "Installing packages (this can take a few minutes)..."
  apt_install "${BASE_PACKAGES[@]}"

  log_info "Installing rspamd..."
  ensure_rspamd

  # Deliberately not stopping postfix/dovecot/rspamd here: each phase that owns one of
  # these services (50/60/70) already restarts it after rendering its own config, so a
  # blanket stop here is unnecessary — and it's actively unsafe on a re-run/repair that
  # only forces phase 10 without also forcing 50/60/70, since nothing would bring the
  # stopped service back up.
  log_info "Disabling and masking sendmail/exim if present (package conflicts)..."
  for alt in exim4 sendmail; do
    systemctl disable --now "$alt" >/dev/null 2>&1 || true
  done

  return 0
}

phase_run
