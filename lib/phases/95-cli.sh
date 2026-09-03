#!/usr/bin/env bash
set -euo pipefail
PATRABAHOK_HOME="${PATRABAHOK_HOME:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
# shellcheck source=../core/log.sh
. "$PATRABAHOK_HOME/lib/core/log.sh"

phase_run() {
  [ -f "$PATRABAHOK_HOME/cli/patrabahok" ] || die "CLI source not found at $PATRABAHOK_HOME/cli/patrabahok"
  install -m 0755 -o root -g root "$PATRABAHOK_HOME/cli/patrabahok" /usr/local/bin/patrabahok
  log_ok "Installed management CLI: /usr/local/bin/patrabahok (try: patrabahok --help)"
  return 0
}

phase_run
