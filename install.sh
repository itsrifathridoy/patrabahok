#!/bin/sh
# patrabahok bootstrap installer — served at https://patrabahok.com/install.sh
#
#   curl -sSL https://patrabahok.com/install.sh | sh -s -- --yes
#
# This script is deliberately small and POSIX-sh only (no bashisms) so it runs the same
# under dash/sh as under bash, and so it can be read in under a minute before you pipe it
# into a root shell. All it does:
#   1. checks you actually mean it (requires --yes when input isn't a terminal)
#   2. resolves a release version (latest by default, or --version vX.Y.Z)
#   3. downloads that release's tarball + SHA-256 checksum from GitHub Releases
#   4. verifies the checksum and refuses to continue on any mismatch
#   5. extracts it under /opt/patrabahok and hands off to the real installer
#
# It does not install or configure any mail server component itself — that all happens
# in the verified release code at bin/patrabahok-installer.
set -eu

REPO="itsrifathridoy/patrabahok"
GITHUB="https://github.com/${REPO}"
API="https://api.github.com/repos/${REPO}"
INSTALL_ROOT="/opt/patrabahok"
CACHE_DIR="/var/cache/patrabahok/download"

log()  { printf '\033[0;36m[boot]\033[0m %s\n' "$*"; }
err()  { printf '\033[0;31m[boot]\033[0m %s\n' "$*" >&2; }
die()  { err "$*"; exit 1; }

YES=0
VERSION="${PATRABAHOK_VERSION:-}"
REMAINING_ARGS=""

# NOTE: forwarded arg values must not contain spaces (e.g. --config /root/answers.env is
# fine; a path with a space in it is not). This is a deliberate POSIX-sh simplification.
while [ $# -gt 0 ]; do
  case "$1" in
    --yes)
      YES=1
      shift
      ;;
    --version)
      VERSION="${2:-}"
      shift 2
      ;;
    --version=*)
      VERSION="${1#--version=}"
      shift
      ;;
    *)
      REMAINING_ARGS="${REMAINING_ARGS} $1"
      shift
      ;;
  esac
done

[ "$(id -u)" = "0" ] || die "This installer must be run as root (e.g. via sudo)."

if [ ! -t 0 ] && [ "$YES" != "1" ]; then
  err "Input is not a terminal (this looks like 'curl | sh') and --yes was not given."
  err "This bootstrap will download and run a checksum-verified release of patrabahok as root."
  err ""
  err "Re-run as:"
  err "  curl -sSL https://patrabahok.com/install.sh | sh -s -- --yes"
  err ""
  err "...or download and read it first, then run it yourself:"
  err "  curl -fsSLO https://patrabahok.com/install.sh && less install.sh && sh install.sh --yes"
  exit 1
fi

ensure_cmd() {
  cmd="$1"; pkg="${2:-$1}"
  command -v "$cmd" >/dev/null 2>&1 && return 0
  log "Installing $pkg..."
  apt-get update -y >/dev/null 2>&1 || true
  apt-get install -y "$pkg" >/dev/null 2>&1 || die "$cmd is required and could not be installed automatically. Install it manually and re-run."
}

ensure_cmd curl curl
ensure_cmd tar tar
ensure_cmd sha256sum coreutils

if [ -z "$VERSION" ]; then
  log "Resolving latest release of ${REPO}..."
  VERSION=$(curl -fsSL "${API}/releases/latest" | grep -m1 '"tag_name"' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
  [ -n "$VERSION" ] || die "Could not resolve the latest release automatically. Pass --version vX.Y.Z explicitly."
fi

log "Installing patrabahok ${VERSION}"

WORKDIR="${CACHE_DIR}/${VERSION}"
mkdir -p "$WORKDIR"
TARBALL="patrabahok-${VERSION}.tar.gz"
TARBALL_URL="${GITHUB}/releases/download/${VERSION}/${TARBALL}"
SHA_URL="${TARBALL_URL}.sha256"

log "Downloading ${TARBALL_URL}"
curl -fsSL -o "${WORKDIR}/${TARBALL}" "$TARBALL_URL" || die "Download failed: $TARBALL_URL"
curl -fsSL -o "${WORKDIR}/${TARBALL}.sha256" "$SHA_URL" || die "Download failed: $SHA_URL"

log "Verifying checksum"
if ! (cd "$WORKDIR" && sha256sum -c "${TARBALL}.sha256") >/dev/null 2>&1; then
  die "Checksum verification FAILED for ${TARBALL}. Aborting — the download may be corrupted or tampered with. Nothing was installed."
fi
log "Checksum OK"

RELEASE_DIR="${INSTALL_ROOT}/releases/${VERSION}"
rm -rf "$RELEASE_DIR"
mkdir -p "$RELEASE_DIR"
log "Extracting to ${RELEASE_DIR}"
tar -xzf "${WORKDIR}/${TARBALL}" -C "$RELEASE_DIR" --strip-components=1

ln -sfn "$RELEASE_DIR" "${INSTALL_ROOT}/current"
chmod +x "${INSTALL_ROOT}/current/bin/patrabahok-installer"

log "Handing off to the main installer (verified release ${VERSION})"
# shellcheck disable=SC2086
exec bash "${INSTALL_ROOT}/current/bin/patrabahok-installer" install ${REMAINING_ARGS}
