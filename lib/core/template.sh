# render_template TEMPLATE_PATH OUTPUT_PATH KEY=VALUE [KEY=VALUE ...]
# Replaces {{KEY}} placeholders in TEMPLATE_PATH with VALUE and writes OUTPUT_PATH.
# Deliberately does NOT touch $VAR / ${VAR} syntax — those belong to the target program
# (e.g. Postfix's own $mydomain macros) and must survive untouched.
render_template() {
  local tmpl="$1" out="$2"
  shift 2
  [ -f "$tmpl" ] || die "Template not found: $tmpl"

  local content
  content="$(cat "$tmpl")"

  local kv key val esc_val
  for kv in "$@"; do
    key="${kv%%=*}"
    val="${kv#*=}"
    esc_val=$(printf '%s' "$val" | sed -e 's/[\/&]/\\&/g')
    content=$(printf '%s\n' "$content" | sed "s/{{${key}}}/${esc_val}/g")
  done

  if printf '%s' "$content" | grep -qE '\{\{[A-Z_]+\}\}'; then
    log_warn "Unsubstituted placeholders remain in rendered ${out}:"
    printf '%s' "$content" | grep -oE '\{\{[A-Z_]+\}\}' | sort -u >&2
  fi

  mkdir -p "$(dirname "$out")"
  printf '%s\n' "$content" > "$out"
}
