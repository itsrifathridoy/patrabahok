#!/usr/bin/env bash
detect_os() {
  if [ ! -f /etc/os-release ]; then
    die "Cannot detect OS: /etc/os-release not found."
  fi
  # shellcheck disable=SC1091
  . /etc/os-release
  OS_ID="${ID:-unknown}"
  OS_VERSION_ID="${VERSION_ID:-unknown}"
  export OS_ID OS_VERSION_ID
}

require_supported_os() {
  detect_os
  if [ "$OS_ID" = "ubuntu" ] && [ "$OS_VERSION_ID" = "24.04" ]; then
    log_ok "Detected supported OS: Ubuntu 24.04"
    return 0
  fi

  log_warn "Detected ${OS_ID} ${OS_VERSION_ID} — this release is tested only on Ubuntu 24.04 LTS."
  log_warn "Other Debian/Ubuntu releases are tracked in docs/ROADMAP.md but not yet validated."
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
