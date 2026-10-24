#!/usr/bin/env bash
# Shared by phases 00-preflight and 90-webmail.

# normalize_webmail_choice VALUE: prints roundcube|custom|none for the accepted spellings,
# or returns 1.
normalize_webmail_choice() {
  case "$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')" in
    roundcube|r|1) printf 'roundcube' ;;
    custom|patrabahok|patrabahok-mail|c|2) printf 'custom' ;;
    none|no|n|3) printf 'none' ;;
    *) return 1 ;;
  esac
}
