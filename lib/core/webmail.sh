#!/usr/bin/env bash
# Shared by phases 00-preflight and 90-webmail.

# normalize_webmail_choice VALUE: prints patrabahok|roundcube|custom|none for the accepted spellings,
# or returns 1.
normalize_webmail_choice() {
  case "$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')" in
    patrabahok|p|1) printf 'patrabahok' ;;
    roundcube|r|2) printf 'roundcube' ;;
    custom|nextjs|c|3) printf 'custom' ;;
    none|no|n|4) printf 'none' ;;
    *) return 1 ;;
  esac
}
