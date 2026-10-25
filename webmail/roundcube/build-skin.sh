#!/usr/bin/env bash
# Builds the patrabahok Roundcube skin's CSS: Elastic's LESS sources (from the pinned
# Roundcube release the installer deploys) compiled together with patrabahok's
# styles/src/_variables.less (palette, type, sizes) and styles/src/_styles.less
# (layout and component redesign), exactly the customization hooks Elastic provides.
# The compiled CSS is committed, so servers never need Node or a build step.
#
# Usage: webmail/roundcube/build-skin.sh [path-to-roundcube-release]
#   Without an argument it downloads the release pinned in lib/phases/90-webmail.sh
#   and verifies its checksum. Needs Docker (Node runs in a container).
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
HERE="$PWD"
SKIN="$HERE/skins/patrabahok"

version="$(grep -m1 '^ROUNDCUBE_VERSION=' ../../lib/phases/90-webmail.sh | cut -d'"' -f2)"
sha256="$(grep -m1 '^ROUNDCUBE_SHA256=' ../../lib/phases/90-webmail.sh | cut -d'"' -f2)"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

src="${1:-}"
if [ -z "$src" ]; then
  echo "Downloading Roundcube ${version}..."
  tarball="roundcubemail-${version}-complete.tar.gz"
  curl -fsSL -o "$work/$tarball" "https://github.com/roundcube/roundcubemail/releases/download/${version}/${tarball}"
  echo "${sha256}  $work/$tarball" | sha256sum -c - >/dev/null || { echo "checksum mismatch" >&2; exit 1; }
  mkdir "$work/rc"
  tar -xzf "$work/$tarball" -C "$work/rc" --strip-components=1
  src="$work/rc"
fi
[ -f "$src/skins/elastic/styles/styles.less" ] || { echo "No Elastic LESS sources under $src" >&2; exit 1; }

# Build tree: a copy of Elastic with patrabahok's override files dropped in.
mkdir -p "$work/build"
cp -r "$src/skins/elastic/." "$work/build/"
cp "$SKIN/styles/src/_variables.less" "$SKIN/styles/src/_styles.less" "$work/build/styles/"

# Docker Desktop on Windows (Git Bash) needs a Windows path for the volume, and Git
# Bash must not rewrite the container-side paths.
host_build="$work/build"
command -v cygpath >/dev/null 2>&1 && host_build="$(cygpath -w "$host_build")"
MSYS_NO_PATHCONV=1 docker run --rm -v "$host_build:/skin" -w /skin node:20-alpine sh -c '
  npm install --silent --no-audit --no-fund -g less@4.2.0 less-plugin-clean-css@1.6.0 >/dev/null 2>&1
  for f in styles print embed; do
    lessc --clean-css="--s1 --advanced" "styles/$f.less" > "styles/$f.min.css"
  done'

# Elastic's fonts and images stay in Elastic's folder; point the compiled CSS there
# (this skin lives next to it, in skins/patrabahok/styles). patrabahok's own font
# (plus-jakarta-*) is in this skin and keeps its relative path.
for f in styles print embed; do
  sed -E \
    -e 's#url\((["'"'"']?)\.\./fonts/(fa-|roboto)#url(\1../../elastic/fonts/\2#g' \
    -e 's#url\((["'"'"']?)\.\./images/#url(\1../../elastic/images/#g' \
    "$work/build/styles/$f.min.css" > "$SKIN/styles/$f.min.css"
done

echo "Built $(wc -c < "$SKIN/styles/styles.min.css") bytes -> skins/patrabahok/styles/styles.min.css (+ print, embed)"
