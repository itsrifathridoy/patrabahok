#!/usr/bin/env bash
set -euo pipefail
PATRABAHOK_HOME="${PATRABAHOK_HOME:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
# shellcheck source=../core/log.sh
. "$PATRABAHOK_HOME/lib/core/log.sh"

# Pinned upstream Go toolchain — deliberately NOT the distro's own golang-go package,
# whose version varies wildly and is often far too old (seen: Go 1.18 on Ubuntu 22.04,
# vs. this module's Go 1.22 requirement). Downloaded fresh into a private, ephemeral
# directory and checksum-verified the same way install.sh verifies its own release —
# never left on disk after the build. Bump both together when changing GO_VERSION:
# checksums come from https://go.dev/dl/?mode=json&include=all for that exact version.
GO_VERSION="1.22.2"
GO_SHA256_AMD64="5901c52b7a78002aeff14a21f93e0f064f74ce1360fce51c6ee68cd471216a17"
GO_SHA256_ARM64="36e720b2d564980c162a48c7e97da2e407dfcc4239e1e58d98082dfa2486a0c1"
GO_TOOLCHAIN_DIR="/opt/patrabahok/.build-go"

ensure_go() {
  local arch sha256 tarball url tmp
  case "$(uname -m)" in
    x86_64) arch=amd64; sha256="$GO_SHA256_AMD64" ;;
    aarch64) arch=arm64; sha256="$GO_SHA256_ARM64" ;;
    *) die "Unsupported CPU architecture for the Go toolchain: $(uname -m)" ;;
  esac
  tarball="go${GO_VERSION}.linux-${arch}.tar.gz"
  url="https://go.dev/dl/${tarball}"

  log_info "Downloading Go ${GO_VERSION} toolchain (${arch}) to build the CLI/API (removed afterward)..."
  rm -rf "$GO_TOOLCHAIN_DIR"
  mkdir -p "$GO_TOOLCHAIN_DIR"
  tmp="$(mktemp -d)"
  curl -fsSL -o "${tmp}/${tarball}" "$url" || die "Failed to download Go toolchain from ${url}"
  if ! echo "${sha256}  ${tmp}/${tarball}" | sha256sum -c - >/dev/null 2>&1; then
    rm -rf "$tmp"
    die "Go toolchain checksum verification FAILED for ${tarball} — aborting, nothing built."
  fi
  tar -C "$GO_TOOLCHAIN_DIR" --strip-components=1 -xzf "${tmp}/${tarball}" || die "Failed to extract Go toolchain."
  rm -rf "$tmp"

  export PATH="${GO_TOOLCHAIN_DIR}/bin:${PATH}"
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
  log_info "Removing the temporary Go toolchain (compiled binaries are self-contained)..."
  rm -rf "$GO_TOOLCHAIN_DIR"
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
