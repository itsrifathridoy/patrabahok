#!/usr/bin/env bash
PATRABAHOK_LOG_DIR="${PATRABAHOK_LOG_DIR:-/var/log/patrabahok}"
PATRABAHOK_LOG_FILE="${PATRABAHOK_LOG_FILE:-$PATRABAHOK_LOG_DIR/install.log}"

_log_init() {
  mkdir -p "$PATRABAHOK_LOG_DIR"
  touch "$PATRABAHOK_LOG_FILE"
  chmod 600 "$PATRABAHOK_LOG_FILE"
}

_log_ts() { date -u '+%Y-%m-%dT%H:%M:%SZ'; }

_log_write() {
  local level="$1"; shift
  printf '%s [%s] %s\n' "$(_log_ts)" "$level" "$*" >> "$PATRABAHOK_LOG_FILE"
}

log_info()  { printf '\033[0;36m[INFO]\033[0m %s\n' "$*"; _log_write INFO "$*"; }
log_ok()    { printf '\033[0;32m[ OK ]\033[0m %s\n' "$*"; _log_write OK "$*"; }
log_warn()  { printf '\033[0;33m[WARN]\033[0m %s\n' "$*" >&2; _log_write WARN "$*"; }
log_error() { printf '\033[0;31m[FAIL]\033[0m %s\n' "$*" >&2; _log_write ERROR "$*"; }

die() {
  log_error "$*"
  exit 1
}

_log_init
