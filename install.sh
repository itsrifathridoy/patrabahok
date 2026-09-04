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
#   3. downloads that release's tarball + SHA-256 checksum + minisign signature
#   4. verifies the checksum (catches corruption/mismatched mirrors) AND the signature
#      (catches a compromised release-publishing process itself — see docs/SECURITY.md)
#      and refuses to continue on any failure of either
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

# The release signing PUBLIC key — safe to embed here. The matching secret key is
# generated and held offline (never in this repo, never in CI); see docs/SECURITY.md
# and scripts/sign-release.sh. Changing this line changes what install.sh trusts, so
# any change to it should be as visible/reviewable as any other line of this script.
RELEASE_PUBKEY="RWSSbLuvGzpoLgY852+0yMVDQedgAL1x+cpSoqeR4Tfmt/1KePaLRhEK"

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

# ensure_minisign — apt has the `minisign` package on Debian 12 and Ubuntu 24.04+, but
# not Ubuntu 22.04 (only a Go *library* package exists there, not the CLI). Falls back
# to the upstream prebuilt binary, checksum-pinned the same way 95-cli.sh pins the Go
# toolchain — deliberately NOT verified via minisign's own .minisig signature, since
# that would be circular (needing minisign to bootstrap minisign) on a host where it
# isn't installed yet. MINISIGN_FALLBACK_VERSION/_SHA256 were obtained from a real
# download, independently reproduced, and functionally tested (sign, verify, and a
# tampered-content verify failure) before being pinned here.
MINISIGN_FALLBACK_VERSION="0.12"
MINISIGN_FALLBACK_SHA256="9a599b48ba6eb7b1e80f12f36b94ceca7c00b7a5173c95c3efc88d9822957e73"

ensure_minisign() {
  command -v minisign >/dev/null 2>&1 && return 0

  log "Installing minisign..."
  apt-get update -y >/dev/null 2>&1 || true
  if apt-get install -y minisign >/dev/null 2>&1 && command -v minisign >/dev/null 2>&1; then
    return 0
  fi

  log "minisign is not packaged for this OS — using the pinned upstream binary instead."
  case "$(uname -m)" in
    x86_64|amd64) arch="x86_64" ;;
    aarch64|arm64) arch="aarch64" ;;
    *) die "Unsupported CPU architecture for the minisign fallback binary: $(uname -m)" ;;
  esac

  tmp="$(mktemp -d)"
  tarball="minisign-${MINISIGN_FALLBACK_VERSION}-linux.tar.gz"
  url="https://github.com/jedisct1/minisign/releases/download/${MINISIGN_FALLBACK_VERSION}/${tarball}"
  curl -fsSL -o "${tmp}/${tarball}" "$url" || die "Failed to download minisign from ${url}"
  if ! echo "${MINISIGN_FALLBACK_SHA256}  ${tmp}/${tarball}" | sha256sum -c - >/dev/null 2>&1; then
    rm -rf "$tmp"
    die "minisign fallback binary checksum verification FAILED — aborting, nothing installed."
  fi
  tar -C "$tmp" -xzf "${tmp}/${tarball}" >/dev/null 2>&1
  install -m 755 "${tmp}/minisign-linux/${arch}/minisign" /usr/local/bin/minisign || die "Failed to install the minisign fallback binary."
  rm -rf "$tmp"
  command -v minisign >/dev/null 2>&1 || die "minisign installation failed unexpectedly."
}

ensure_cmd curl curl
ensure_cmd tar tar
ensure_cmd sha256sum coreutils
ensure_minisign

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
SIG_URL="${TARBALL_URL}.minisig"

log "Downloading ${TARBALL_URL}"
curl -fsSL -o "${WORKDIR}/${TARBALL}" "$TARBALL_URL" || die "Download failed: $TARBALL_URL"
curl -fsSL -o "${WORKDIR}/${TARBALL}.sha256" "$SHA_URL" || die "Download failed: $SHA_URL"
curl -fsSL -o "${WORKDIR}/${TARBALL}.minisig" "$SIG_URL" || die "Download failed: $SIG_URL"

log "Verifying checksum"
if ! (cd "$WORKDIR" && sha256sum -c "${TARBALL}.sha256") >/dev/null 2>&1; then
  die "Checksum verification FAILED for ${TARBALL}. Aborting — the download may be corrupted or tampered with. Nothing was installed."
fi
log "Checksum OK"

log "Verifying release signature"
if ! minisign -Vm "${WORKDIR}/${TARBALL}" -P "$RELEASE_PUBKEY" -x "${WORKDIR}/${TARBALL}.minisig" -q; then
  die "Signature verification FAILED for ${TARBALL}. Aborting — this release was not validly signed by the patrabahok release key. Nothing was installed."
fi
log "Signature OK"

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
