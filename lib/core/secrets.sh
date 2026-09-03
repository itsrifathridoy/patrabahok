#!/usr/bin/env bash
PATRABAHOK_SECRETS_FILE="${PATRABAHOK_SECRETS_FILE:-/etc/patrabahok/secrets.env}"

gen_secret() {
  # Deliberately not "openssl ... | tr ... | cut -c1-32": with pipefail active, cut
  # closing its input early after reading 32 bytes sends SIGPIPE upstream, which
  # pipefail turns into a nonzero pipeline status and aborts the caller under set -e.
  local raw
  raw=$(openssl rand -base64 40 | tr -dc 'A-Za-z0-9')
  printf '%s' "${raw:0:32}"
}

secrets_init() {
  mkdir -p "$(dirname "$PATRABAHOK_SECRETS_FILE")"
  chmod 700 "$(dirname "$PATRABAHOK_SECRETS_FILE")"
  if [ ! -f "$PATRABAHOK_SECRETS_FILE" ]; then
    umask 077
    : > "$PATRABAHOK_SECRETS_FILE"
    chmod 600 "$PATRABAHOK_SECRETS_FILE"
  fi
}

# secret_ensure NAME — sets shell var $NAME to the existing persisted value, or generates
# and persists a new one. Idempotent: safe to call on every re-run.
secret_ensure() {
  local name="$1"
  secrets_init
  local current
  # "|| true" is required: grep exits 1 on no-match (the normal first-run case), and
  # under pipefail that nonzero status propagates through the pipe even though cut
  # itself succeeds, which would otherwise abort the script here via set -e.
  current=$(grep -m1 "^${name}=" "$PATRABAHOK_SECRETS_FILE" 2>/dev/null | cut -d= -f2- || true)
  if [ -n "$current" ]; then
    printf -v "$name" '%s' "$current"
    return 0
  fi
  local value
  value="$(gen_secret)"
  printf -v "$name" '%s' "$value"
  printf '%s=%s\n' "$name" "$value" >> "$PATRABAHOK_SECRETS_FILE"
  chmod 600 "$PATRABAHOK_SECRETS_FILE"
}

secrets_load() {
  secrets_init
  set -a
  # shellcheck disable=SC1090
  . "$PATRABAHOK_SECRETS_FILE"
  set +a
}
