#!/usr/bin/env bash
set -euo pipefail
PATRABAHOK_HOME="${PATRABAHOK_HOME:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
# shellcheck source=../core/log.sh
. "$PATRABAHOK_HOME/lib/core/log.sh"
# shellcheck source=../core/os.sh
. "$PATRABAHOK_HOME/lib/core/os.sh"

GO_INSTALLED_BY_US=0

ensure_go() {
  if command -v go >/dev/null 2>&1; then
    return 0
  fi
  log_info "Installing a temporary Go toolchain to build the CLI/API (removed afterward)..."
  apt_update
  apt_install golang-go
  GO_INSTALLED_BY_US=1
}

build_go_binaries() {
  local src="$PATRABAHOK_HOME/cli"
  [ -f "$src/go.mod" ] || die "Go module source not found at $src"

  export CGO_ENABLED=0
  export GOFLAGS="-trimpath"
  export HOME="${HOME:-/root}"

  (
    cd "$src"
    if [ -f go.sum ]; then
      log_info "Building patrabahok CLI/API (pinned dependencies via go.sum)..."
    else
      log_warn "No go.sum present — resolving dependencies from the network (not pinned). This should not happen in a released tarball; see docs/ROADMAP.md."
      go mod tidy
    fi
    go build -o /usr/local/bin/patrabahok ./cmd/patrabahok
    go build -o /usr/local/bin/patrabahokd ./cmd/patrabahokd
  ) || die "Failed to build the Go CLI/API. See output above."

  chmod 0755 /usr/local/bin/patrabahok /usr/local/bin/patrabahokd
}

cleanup_go() {
  if [ "$GO_INSTALLED_BY_US" -eq 1 ]; then
    log_info "Removing the temporary Go toolchain (compiled binaries are self-contained)..."
    apt-get purge -y golang-go >/dev/null 2>&1 || true
    apt-get autoremove -y >/dev/null 2>&1 || true
  fi
}

phase_run() {
  ensure_go
  build_go_binaries
  cleanup_go

  getent group patrabahok >/dev/null 2>&1 || groupadd -r patrabahok

  install -m 0644 "$PATRABAHOK_HOME/templates/systemd/patrabahokd.service" /etc/systemd/system/patrabahokd.service
  systemctl daemon-reload
  systemctl enable --now patrabahokd

  local tries=0
  until [ -S /run/patrabahok/api.sock ]; do
    tries=$((tries + 1))
    [ "$tries" -gt 15 ] && { log_warn "patrabahokd socket did not appear in time — check 'journalctl -u patrabahokd'."; break; }
    sleep 1
  done

  log_ok "Installed patrabahok CLI (/usr/local/bin/patrabahok) and patrabahokd API (unix:///run/patrabahok/api.sock)"
  return 0
}

phase_run
