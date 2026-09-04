#!/usr/bin/env bash
# Builds the release tarball install.sh downloads, from a clean `git archive` of the
# current HEAD — not whatever happens to be sitting in the working tree (including
# untracked/ignored files) — so what gets signed is provably exactly what's in version
# control at that commit.
#
# Usage: scripts/build-release.sh
# Reads the version from ./VERSION (e.g. "0.1.0"), writes:
#   dist/patrabahok-v<version>.tar.gz
#   dist/patrabahok-v<version>.tar.gz.sha256
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

VERSION="$(tr -d ' \t\n\r' < VERSION)"
[ -n "$VERSION" ] || { echo "VERSION file is empty" >&2; exit 1; }
TAG="v${VERSION}"

OUT_DIR="dist"
TARBALL="patrabahok-${TAG}.tar.gz"

mkdir -p "$OUT_DIR"
rm -f "${OUT_DIR}/${TARBALL}" "${OUT_DIR}/${TARBALL}.sha256"

# --prefix matches install.sh's `tar -xzf ... --strip-components=1` — the archive must
# have exactly one leading path component for that to land files correctly.
git archive --format=tar.gz --prefix="patrabahok-${TAG}/" -o "${OUT_DIR}/${TARBALL}" HEAD

(cd "$OUT_DIR" && sha256sum "$TARBALL" > "${TARBALL}.sha256")

echo "Built ${OUT_DIR}/${TARBALL}"
cat "${OUT_DIR}/${TARBALL}.sha256"
echo
echo "Next: scripts/sign-release.sh ${OUT_DIR}/${TARBALL} <path-to-your-secret-key>"
