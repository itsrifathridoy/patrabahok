#!/usr/bin/env bash
set -euo pipefail
PATRABAHOK_HOME="${PATRABAHOK_HOME:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
# shellcheck source=../core/log.sh
. "$PATRABAHOK_HOME/lib/core/log.sh"
# shellcheck source=../core/state.sh
. "$PATRABAHOK_HOME/lib/core/state.sh"
# shellcheck source=../core/secrets.sh
. "$PATRABAHOK_HOME/lib/core/secrets.sh"
state_init

DB_NAME="mailserver"
DB_READONLY_USER="mailuser"
DB_ADMIN_USER="patrabahok"

phase_run() {
  log_info "Starting MariaDB..."
  systemctl enable --now mariadb >/dev/null 2>&1
  systemctl restart mariadb

  local tries=0
  until mysqladmin ping --silent 2>/dev/null; do
    tries=$((tries + 1))
    [ "$tries" -gt 30 ] && die "MariaDB did not come up in time."
    sleep 1
  done

  log_info "Hardening MariaDB (removing anonymous users/test db)..."
  local this_host
  this_host="$(hostname)"
  mysql -u root <<SQL
DROP USER IF EXISTS ''@'localhost';
DROP USER IF EXISTS ''@'${this_host}';
DROP DATABASE IF EXISTS test;
FLUSH PRIVILEGES;
SQL

  secret_ensure MAIL_DB_READONLY_PASSWORD
  secret_ensure MAIL_DB_ADMIN_PASSWORD

  log_info "Creating database '${DB_NAME}' and applying schema..."
  mysql -u root -e "CREATE DATABASE IF NOT EXISTS \`${DB_NAME}\` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;"

  local f
  for f in "$PATRABAHOK_HOME"/sql/schema/*.sql; do
    [ -e "$f" ] || continue
    log_info "Applying $(basename "$f")..."
    mysql -u root "$DB_NAME" < "$f"
  done

  log_info "Creating/updating database users..."
  mysql -u root <<SQL
CREATE USER IF NOT EXISTS '${DB_READONLY_USER}'@'localhost' IDENTIFIED BY '${MAIL_DB_READONLY_PASSWORD}';
ALTER USER '${DB_READONLY_USER}'@'localhost' IDENTIFIED BY '${MAIL_DB_READONLY_PASSWORD}';
GRANT SELECT ON \`${DB_NAME}\`.* TO '${DB_READONLY_USER}'@'localhost';

CREATE USER IF NOT EXISTS '${DB_ADMIN_USER}'@'localhost' IDENTIFIED BY '${MAIL_DB_ADMIN_PASSWORD}';
ALTER USER '${DB_ADMIN_USER}'@'localhost' IDENTIFIED BY '${MAIL_DB_ADMIN_PASSWORD}';
GRANT SELECT, INSERT, UPDATE, DELETE ON \`${DB_NAME}\`.* TO '${DB_ADMIN_USER}'@'localhost';

FLUSH PRIVILEGES;
SQL

  state_set db_name "$DB_NAME"
  state_set db_readonly_user "$DB_READONLY_USER"
  state_set db_admin_user "$DB_ADMIN_USER"

  log_info "Writing CLI database credentials file..."
  cat > /etc/patrabahok/mysql-admin.cnf <<EOF
[client]
user=${DB_ADMIN_USER}
password=${MAIL_DB_ADMIN_PASSWORD}
host=127.0.0.1
database=${DB_NAME}
EOF
  chmod 600 /etc/patrabahok/mysql-admin.cnf

  log_ok "Database ready: ${DB_NAME} (readonly user: ${DB_READONLY_USER}, admin user: ${DB_ADMIN_USER})"
  return 0
}

phase_run
