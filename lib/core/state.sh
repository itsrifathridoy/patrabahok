PATRABAHOK_ETC_DIR="${PATRABAHOK_ETC_DIR:-/etc/patrabahok}"
PATRABAHOK_STATE_FILE="${PATRABAHOK_STATE_FILE:-$PATRABAHOK_ETC_DIR/state.json}"

state_init() {
  command -v jq >/dev/null 2>&1 || die "jq is required but not installed. It should have been installed automatically before phases ran — install it manually (apt-get install -y jq) and re-run."
  mkdir -p "$PATRABAHOK_ETC_DIR"
  chmod 700 "$PATRABAHOK_ETC_DIR"
  if [ ! -f "$PATRABAHOK_STATE_FILE" ]; then
    jq -n --arg version "${PATRABAHOK_INSTALLER_VERSION:-dev}" \
      '{version: $version, phases: {}, config: {}}' > "$PATRABAHOK_STATE_FILE"
    chmod 600 "$PATRABAHOK_STATE_FILE"
  fi
}

state_is_done() {
  local phase="$1"
  [ -f "$PATRABAHOK_STATE_FILE" ] || return 1
  local status
  status=$(jq -r --arg p "$phase" '.phases[$p].status // "none"' "$PATRABAHOK_STATE_FILE")
  [ "$status" = "done" ]
}

state_mark_done() {
  local phase="$1"
  local tmp; tmp=$(mktemp)
  jq --arg p "$phase" --arg at "$(_log_ts 2>/dev/null || date -u '+%Y-%m-%dT%H:%M:%SZ')" \
    '.phases[$p] = {status:"done", at:$at}' "$PATRABAHOK_STATE_FILE" > "$tmp"
  mv "$tmp" "$PATRABAHOK_STATE_FILE"
  chmod 600 "$PATRABAHOK_STATE_FILE"
}

state_mark_failed() {
  local phase="$1" err="${2:-unknown error}"
  local tmp; tmp=$(mktemp)
  jq --arg p "$phase" --arg at "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" --arg err "$err" \
    '.phases[$p] = {status:"failed", at:$at, error:$err}' "$PATRABAHOK_STATE_FILE" > "$tmp"
  mv "$tmp" "$PATRABAHOK_STATE_FILE"
  chmod 600 "$PATRABAHOK_STATE_FILE"
}

# state_set KEY VALUE — persists a config value so later phases (separate processes) can read it
state_set() {
  local key="$1" value="$2"
  local tmp; tmp=$(mktemp)
  jq --arg k "$key" --arg v "$value" '.config[$k] = $v' "$PATRABAHOK_STATE_FILE" > "$tmp"
  mv "$tmp" "$PATRABAHOK_STATE_FILE"
  chmod 600 "$PATRABAHOK_STATE_FILE"
}

state_get() {
  local key="$1" default="${2:-}"
  if [ ! -f "$PATRABAHOK_STATE_FILE" ]; then
    printf '%s' "$default"
    return 0
  fi
  local v
  v=$(jq -r --arg k "$key" '.config[$k] // empty' "$PATRABAHOK_STATE_FILE")
  if [ -n "$v" ]; then printf '%s' "$v"; else printf '%s' "$default"; fi
}

# state_set_list KEY  — appends VALUE to a JSON array at .config[KEY] if not already present
state_add_to_list() {
  local key="$1" value="$2"
  local tmp; tmp=$(mktemp)
  jq --arg k "$key" --arg v "$value" \
    '.config[$k] = ((.config[$k] // []) + [$v] | unique)' "$PATRABAHOK_STATE_FILE" > "$tmp"
  mv "$tmp" "$PATRABAHOK_STATE_FILE"
  chmod 600 "$PATRABAHOK_STATE_FILE"
}

state_get_list() {
  local key="$1"
  [ -f "$PATRABAHOK_STATE_FILE" ] || return 0
  jq -r --arg k "$key" '.config[$k] // [] | .[]' "$PATRABAHOK_STATE_FILE"
}
