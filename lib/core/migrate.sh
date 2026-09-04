#!/usr/bin/env bash
# apply_pending_migrations — applies any sql/schema/*.sql file whose version isn't yet
# recorded in schema_migrations. Called unconditionally on every installer invocation
# (not gated by phase 30-database's done-state), so a later release adding a new
# migration file actually reaches an already-installed server on the next re-run of
# install.sh, rather than being silently skipped because 30-database is already "done".
# Deliberately best-effort/silent on a fresh install where MariaDB/the database don't
# exist yet — phase 30-database's own schema-apply loop handles that first-run case.
apply_pending_migrations() {
  command -v mysql >/dev/null 2>&1 || return 0
  systemctl is-active --quiet mariadb 2>/dev/null || return 0

  local db_name
  db_name="$(state_get db_name)"
  [ -n "$db_name" ] || return 0
  mysql -u root -e "SELECT 1;" >/dev/null 2>&1 || return 0
  mysql -u root "$db_name" -e "SELECT 1 FROM schema_migrations LIMIT 1;" >/dev/null 2>&1 || return 0

  local f base version applied
  for f in "$PATRABAHOK_HOME"/sql/schema/*.sql; do
    [ -e "$f" ] || continue
    base="$(basename "$f")"
    version="${base%%_*}"
    case "$version" in
      ''|*[!0-9]*) continue ;;
    esac
    version=$((10#$version))

    applied="$(mysql -u root -N -B "$db_name" -e "SELECT 1 FROM schema_migrations WHERE version=${version};" 2>/dev/null || true)"
    [ "$applied" = "1" ] && continue

    log_info "Applying pending database migration: ${base}"
    mysql -u root "$db_name" < "$f" || log_warn "Migration ${base} did not apply cleanly — check manually (mysql -u root ${db_name} < sql/schema/${base})."
  done
}
