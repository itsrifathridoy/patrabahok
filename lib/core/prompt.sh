#!/usr/bin/env bash
PATRABAHOK_NON_INTERACTIVE="${PATRABAHOK_NON_INTERACTIVE:-0}"

declare -a _PATRABAHOK_MISSING_VARS=()

# ask VAR "Question text" ["default"]
# Sets shell variable $VAR. Precedence: existing PATRABAHOK_<VAR> env var > interactive
# prompt (with default) > default (non-interactive) > record as missing (non-interactive, no default).
ask() {
  local __var="$1" __question="$2" __default="${3:-}"
  local __envname="PATRABAHOK_${__var}"
  local __current="${!__envname:-}"

  if [ -n "$__current" ]; then
    printf -v "$__var" '%s' "$__current"
    return 0
  fi

  if [ "$PATRABAHOK_NON_INTERACTIVE" = "1" ]; then
    if [ -n "$__default" ]; then
      printf -v "$__var" '%s' "$__default"
      return 0
    fi
    _PATRABAHOK_MISSING_VARS+=("$__envname")
    printf -v "$__var" '%s' ""
    return 0
  fi

  local __reply=""
  if [ -n "$__default" ]; then
    read -r -p "$__question [$__default]: " __reply < /dev/tty
    __reply="${__reply:-$__default}"
  else
    while [ -z "$__reply" ]; do
      read -r -p "$__question: " __reply < /dev/tty
    done
  fi
  printf -v "$__var" '%s' "$__reply"
}

# confirm "Question" [y|n default]
confirm() {
  local __question="$1" __default="${2:-y}"
  if [ "$PATRABAHOK_NON_INTERACTIVE" = "1" ]; then
    [ "$__default" = "y" ]
    return $?
  fi
  local __hint="y/N"
  [ "$__default" = "y" ] && __hint="Y/n"
  local __reply=""
  read -r -p "$__question [$__hint]: " __reply < /dev/tty || true
  __reply="${__reply:-$__default}"
  case "$__reply" in
    y|Y|yes|YES) return 0 ;;
    *) return 1 ;;
  esac
}

# ask_finalize — call after all ask() calls in a phase to hard-fail (once, listing everything
# missing) if running --non-interactive without required values supplied.
ask_finalize() {
  if [ "${#_PATRABAHOK_MISSING_VARS[@]}" -gt 0 ]; then
    log_error "Non-interactive mode is missing required variables:"
    local v
    for v in "${_PATRABAHOK_MISSING_VARS[@]}"; do
      printf '  - %s\n' "$v" >&2
    done
    die "Set these as environment variables, or via --config FILE, then re-run."
  fi
}
