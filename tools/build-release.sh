#!/usr/bin/env bash
# Packages a release tarball + checksum matching what install.sh expects to download
# from GitHub Releases. Run this, then `gh release create` with the two output files.
#
# Usage: tools/build-release.sh [vX.Y.Z]   (defaults to v<contents of VERSION file>)
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

VERSION="${1:-v$(cat VERSION)}"
STAGE_NAME="patrabahok-${VERSION}"

rm -rf dist
mkdir -p "dist/${STAGE_NAME}"

cp -r bin lib templates sql cli docs "dist/${STAGE_NAME}/"
cp VERSION LICENSE README.md CHANGELOG.md install.sh "dist/${STAGE_NAME}/"
chmod +x "dist/${STAGE_NAME}/bin/patrabahok-installer" "dist/${STAGE_NAME}/cli/patrabahok" "dist/${STAGE_NAME}/install.sh"

(
  cd dist
  tar -czf "${STAGE_NAME}.tar.gz" "${STAGE_NAME}"
  sha256sum "${STAGE_NAME}.tar.gz" > "${STAGE_NAME}.tar.gz.sha256"
  rm -rf "${STAGE_NAME}"
)

echo "Built: dist/${STAGE_NAME}.tar.gz"
echo "Checksum: $(cat "dist/${STAGE_NAME}.tar.gz.sha256")"
echo
echo "Next steps:"
echo "  git tag ${VERSION} && git push origin ${VERSION}"
echo "  gh release create ${VERSION} dist/${STAGE_NAME}.tar.gz dist/${STAGE_NAME}.tar.gz.sha256 --title ${VERSION} --notes-file CHANGELOG.md"
