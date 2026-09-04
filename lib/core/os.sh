#!/usr/bin/env bash
detect_os() {
  if [ ! -f /etc/os-release ]; then
    die "Cannot detect OS: /etc/os-release not found."
  fi
  # shellcheck disable=SC1091
  . /etc/os-release
  OS_ID="${ID:-unknown}"
  OS_VERSION_ID="${VERSION_ID:-unknown}"
  OS_CODENAME="${VERSION_CODENAME:-unknown}"
  export OS_ID OS_VERSION_ID OS_CODENAME
}

# Supported OS/version combinations. Keep in sync with docs/ROADMAP.md and README.
is_supported_os() {
  case "${OS_ID}/${OS_VERSION_ID}" in
    ubuntu/24.04|ubuntu/22.04|debian/12) return 0 ;;
    *) return 1 ;;
  esac
}

require_supported_os() {
  detect_os
  if is_supported_os; then
    log_ok "Detected supported OS: ${OS_ID} ${OS_VERSION_ID} (${OS_CODENAME})"
    return 0
  fi

  log_warn "Detected ${OS_ID} ${OS_VERSION_ID} — supported targets are Ubuntu 24.04, Ubuntu 22.04, and Debian 12."
  log_warn "Other Debian/Ubuntu releases are tracked in docs/ROADMAP.md but not yet supported."
  if [ "${PATRABAHOK_FORCE_OS:-0}" = "1" ]; then
    log_warn "Continuing anyway because --force-os was given. Expect rough edges."
    return 0
  fi
  die "Unsupported OS. Re-run with --force-os to continue anyway at your own risk."
}

apt_update() {
  DEBIAN_FRONTEND=noninteractive apt-get update -y
}

apt_install() {
  DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends "$@"
}

# detect_rspamd_user — the system user rspamd's service actually runs as. Debian/Ubuntu's
# own packages use "_rspamd"; this only falls back to guessing if rspamd isn't installed
# via one of those (e.g. the ensure_rspamd() fallback to the official rspamd.com repo).
detect_rspamd_user() {
  local u
  u="$(systemctl show rspamd -p User --value 2>/dev/null)"
  if [ -z "$u" ] || [ "$u" = "root" ]; then
    if getent passwd _rspamd >/dev/null 2>&1; then
      u="_rspamd"
    elif getent passwd rspamd >/dev/null 2>&1; then
      u="rspamd"
    else
      u="_rspamd"
    fi
  fi
  printf '%s' "$u"
}
