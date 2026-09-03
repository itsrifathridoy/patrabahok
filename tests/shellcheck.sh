#!/usr/bin/env bash
# Lints every shell script in the repo. Run locally or from CI (.github/workflows/lint.yml).
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

command -v shellcheck >/dev/null 2>&1 || {
  echo "shellcheck is not installed. On Debian/Ubuntu: apt-get install shellcheck" >&2
  exit 1
}

files=(install.sh bin/patrabahok-installer cli/patrabahok)
while IFS= read -r -d '' f; do files+=("$f"); done < <(find lib -name '*.sh' -print0)

fail=0
for f in "${files[@]}"; do
  echo "== $f =="
  shellcheck -x "$f" || fail=1
done

exit "$fail"
