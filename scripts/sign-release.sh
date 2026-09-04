#!/usr/bin/env bash
# Signs a built release tarball with the patrabahok release signing key.
#
# Run this OFFLINE, by hand, from your own machine holding the secret key — never in
# CI, and never with the key checked into this repo. That separation is the entire
# point: a compromised GitHub Actions pipeline or token can publish release assets, but
# it cannot produce a validly-signed release without this key, which it never has
# access to. See docs/SECURITY.md.
#
# Usage: scripts/sign-release.sh <tarball> <secret-key-file>
set -euo pipefail

if [ $# -ne 2 ]; then
  echo "Usage: $0 <tarball> <secret-key-file>" >&2
  exit 1
fi

TARBALL="$1"
SECKEY="$2"

command -v minisign >/dev/null 2>&1 || { echo "minisign is required (apt install minisign / brew install minisign)." >&2; exit 1; }
[ -f "$TARBALL" ] || { echo "Not found: $TARBALL" >&2; exit 1; }
[ -f "$SECKEY" ] || { echo "Not found: $SECKEY" >&2; exit 1; }

minisign -S -s "$SECKEY" -m "$TARBALL" -t "patrabahok release: $(basename "$TARBALL")"

echo
echo "Signed: ${TARBALL}.minisig"
echo
echo "Upload as release assets, alongside the existing checksum:"
echo "  ${TARBALL}"
echo "  ${TARBALL}.sha256"
echo "  ${TARBALL}.minisig"
