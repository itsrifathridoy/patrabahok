#!/usr/bin/env bash
# Installs the webmail client chosen in 00-preflight (state "webmail"):
#   patrabahok (default) — the Roundcube engine with patrabahok's skin and plugin
#                          (webmail/roundcube/ in this repo) overlaid on the pinned release
#   roundcube            — stock Roundcube (Elastic skin)
#   custom               — the Next.js client in mail-client/
#   none Either way the webmail only
# listens on loopback. patrabahokd's public :443 listener terminates TLS and
# reverse-proxies to the upstream recorded here as state "webmail_upstream" (see
# cli/internal/frontdoor).
set -euo pipefail
PATRABAHOK_HOME="${PATRABAHOK_HOME:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
# shellcheck source=../core/log.sh
. "$PATRABAHOK_HOME/lib/core/log.sh"
# shellcheck source=../core/os.sh
. "$PATRABAHOK_HOME/lib/core/os.sh"
# shellcheck source=../core/state.sh
. "$PATRABAHOK_HOME/lib/core/state.sh"
# shellcheck source=../core/secrets.sh
. "$PATRABAHOK_HOME/lib/core/secrets.sh"
# shellcheck source=../core/prompt.sh
. "$PATRABAHOK_HOME/lib/core/prompt.sh"
# shellcheck source=../core/template.sh
. "$PATRABAHOK_HOME/lib/core/template.sh"
# shellcheck source=../core/webmail.sh
. "$PATRABAHOK_HOME/lib/core/webmail.sh"
# shellcheck source=../core/patrabahokd.sh
. "$PATRABAHOK_HOME/lib/core/patrabahokd.sh"
state_init

# Pinned upstream Roundcube "complete" release (dependencies bundled, no composer run on
# the server). Deliberately not the distro's roundcube package: its version differs on
# every supported OS, and it pulls in Apache plus interactive dbconfig-common prompts. The
# checksum was cross-checked against both the GitHub release download and the one
# published on roundcube.net/download. Bump the version and checksum together.
ROUNDCUBE_VERSION="1.7.5"
ROUNDCUBE_SHA256="93a66c94e3efff7db93187c1c9d7f35546647a3e1e7b9327cc87911564edf9d1"
ROUNDCUBE_ROOT="/opt/roundcube"
ROUNDCUBE_PORT=8081
ROUNDCUBE_DB_NAME="roundcube"

# Pinned upstream Node.js LTS for patrabahok Mail. The distro packages are too old for
# Next.js 15 (Ubuntu 22.04 ships Node 12). Checksums come from
# https://nodejs.org/dist/v<version>/SHASUMS256.txt for exactly this version.
NODE_VERSION="22.23.4"
NODE_SHA256_X64="1805bcd35e6815559bd2eb97db438d3fdb0eb3a5e6a977b7c1badca76337e9ce"
NODE_SHA256_ARM64="6287aa46b6c375d816e63ae66d024fbbfd70404825aabdfdf898a970871d2f4b"
NODE_DIR="/opt/patrabahok/node"
MAIL_CLIENT_DIR="/opt/patrabahok-mail"
MAIL_CLIENT_PORT=3000
MAIL_CLIENT_DB_NAME="patrabahok_mail"
MAIL_CLIENT_DB_USER="patrabahok_mail"

choose_webmail() {
  local current
  current="$(state_get webmail)"
  # Re-asked only if there's no recorded choice (an install from a release that predates
  # the question) or PATRABAHOK_WEBMAIL is set explicitly to switch.
  if [ -n "$current" ] && [ -z "${PATRABAHOK_WEBMAIL:-}" ]; then
    printf '%s' "$current"
    return 0
  fi
  ask WEBMAIL "Webmail client (patrabahok / roundcube / custom / none)" "patrabahok"
  ask_finalize
  normalize_webmail_choice "$WEBMAIL" || die "Invalid webmail choice: $WEBMAIL (expected patrabahok, roundcube, custom, or none)"
}

# download_verified URL SHA256 OUTPUT
download_verified() {
  local url="$1" sha256="$2" out="$3"
  curl -fsSL -o "$out" "$url" || die "Failed to download ${url}"
  if ! echo "${sha256}  ${out}" | sha256sum -c - >/dev/null 2>&1; then
    rm -f "$out"
    die "Checksum verification FAILED for ${url}. Aborting, nothing installed."
  fi
}

# --- Roundcube ----------------------------------------------------------------------

# setup_managesieve: Dovecot's ManageSieve service, bound to loopback, so webmail users
# can manage their own filters, vacation replies and forwarding (Roundcube's managesieve
# plugin). Scripts live where Dovecot already looks (sieve = file:~/sieve).
setup_managesieve() {
  log_info "Enabling Dovecot ManageSieve (loopback only) for filters and vacation replies..."
  apt_install dovecot-managesieved
  # Named to sort after 99-patrabahok.conf, which sets "protocols = imap lmtp".
  cat > /etc/dovecot/conf.d/99-zz-patrabahok-managesieve.conf <<'EOF'
# Managed by patrabahok (lib/phases/90-webmail.sh). ManageSieve for webmail only: bound
# to loopback and never opened in the firewall.
protocols = $protocols sieve
service managesieve-login {
  inet_listener sieve {
    address = 127.0.0.1
    port = 4190
  }
}
EOF
  doveconf -n >/dev/null 2>&1 || { doveconf -n; die "Dovecot configuration test failed after enabling ManageSieve."; }
  systemctl restart dovecot
}

# install_roundcube FLAVOR — FLAVOR is "patrabahok" (patrabahok skin + plugin) or "roundcube".
install_roundcube() {
  local flavor="${1:-patrabahok}"
  local hostname mail_db_name
  hostname="$(state_get hostname)"
  mail_db_name="$(state_get db_name)"
  [ -n "$hostname" ] && [ -n "$mail_db_name" ] || die "hostname/db_name missing from state. Run the earlier phases first."

  log_info "Installing nginx and PHP-FPM for Roundcube..."
  apt_install nginx php-fpm php-cli php-mysql php-intl php-mbstring php-xml php-zip php-gd php-curl media-types curl
  # The nginx package enables a default site on :80, and certbot's standalone renewal
  # (phase 40-tls) needs that port free. Only the loopback site below stays enabled.
  rm -f /etc/nginx/sites-enabled/default

  local php_ver fpm_sock
  php_ver="$(php -r 'echo PHP_MAJOR_VERSION.".".PHP_MINOR_VERSION;')"
  fpm_sock="/run/php/php${php_ver}-fpm.sock"

  local release="${ROUNDCUBE_ROOT}/releases/${ROUNDCUBE_VERSION}"
  if [ ! -f "${release}/index.php" ]; then
    log_info "Downloading Roundcube ${ROUNDCUBE_VERSION}..."
    local tmp tarball="roundcubemail-${ROUNDCUBE_VERSION}-complete.tar.gz"
    tmp="$(mktemp -d)"
    download_verified "https://github.com/roundcube/roundcubemail/releases/download/${ROUNDCUBE_VERSION}/${tarball}" \
      "$ROUNDCUBE_SHA256" "${tmp}/${tarball}"
    rm -rf "$release"
    mkdir -p "$release"
    tar -C "$release" --strip-components=1 -xzf "${tmp}/${tarball}" || die "Failed to extract Roundcube."
    rm -rf "$tmp" "${release}/installer"
    chown -R root:root "$release"
  fi
  ln -sfn "$release" "${ROUNDCUBE_ROOT}/current"

  # The patrabahok skin and plugin come from this repo and are refreshed on every run,
  # so an upgrade picks up their changes even when the Roundcube release is unchanged.
  local skin="elastic"
  local plugins="'archive', 'zipdownload', 'password', 'managesieve', 'markasjunk', 'newmail_notifier', 'attachment_reminder', 'emoticons', 'hide_blockquote', 'vcard_attachments'"
  local product="Webmail"
  if [ "$flavor" = "patrabahok" ]; then
    local src="$PATRABAHOK_HOME/webmail/roundcube"
    [ -f "$src/skins/patrabahok/meta.json" ] || die "patrabahok webmail skin not found at $src"
    rm -rf "${release}/skins/patrabahok" "${release}/plugins/patrabahok"
    cp -r "$src/skins/patrabahok" "${release}/skins/patrabahok"
    cp -r "$src/plugins/patrabahok" "${release}/plugins/patrabahok"
    rm -rf "${release}/skins/patrabahok/styles/src"
    chown -R root:root "${release}/skins/patrabahok" "${release}/plugins/patrabahok"
    skin="patrabahok"
    plugins="'patrabahok', ${plugins}"
    product="patrabahok Webmail"
  fi

  mkdir -p /var/log/roundcube /var/lib/roundcube/temp
  touch /var/log/roundcube/errors.log
  chown -R www-data:www-data /var/log/roundcube /var/lib/roundcube
  chmod 750 /var/log/roundcube
  chmod 700 /var/lib/roundcube/temp

  secret_ensure ROUNDCUBE_DB_PASSWORD
  secret_ensure ROUNDCUBE_PW_DB_PASSWORD
  secret_ensure ROUNDCUBE_DES_KEY

  log_info "Creating the Roundcube database..."
  mysql -u root <<SQL
CREATE DATABASE IF NOT EXISTS \`${ROUNDCUBE_DB_NAME}\` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER IF NOT EXISTS 'roundcube'@'localhost' IDENTIFIED BY '${ROUNDCUBE_DB_PASSWORD}';
ALTER USER 'roundcube'@'localhost' IDENTIFIED BY '${ROUNDCUBE_DB_PASSWORD}';
GRANT ALL PRIVILEGES ON \`${ROUNDCUBE_DB_NAME}\`.* TO 'roundcube'@'localhost';

-- Roundcube's password plugin: read just enough to match the mailbox, write only its hash.
CREATE USER IF NOT EXISTS 'roundcube_pw'@'localhost' IDENTIFIED BY '${ROUNDCUBE_PW_DB_PASSWORD}';
ALTER USER 'roundcube_pw'@'localhost' IDENTIFIED BY '${ROUNDCUBE_PW_DB_PASSWORD}';
GRANT SELECT (email, enabled), UPDATE (password) ON \`${mail_db_name}\`.\`virtual_users\` TO 'roundcube_pw'@'localhost';
FLUSH PRIVILEGES;
SQL

  log_info "Writing Roundcube configuration..."
  # des_key encrypts IMAP passwords in sessions and must be exactly 24 characters.
  render_template "$PATRABAHOK_HOME/templates/roundcube/config.inc.php.tmpl" "${release}/config/config.inc.php" \
    "DB_PASSWORD=${ROUNDCUBE_DB_PASSWORD}" "MAIL_HOSTNAME=${hostname}" "DES_KEY=${ROUNDCUBE_DES_KEY:0:24}" \
    "PRODUCT_NAME=${product}" "SKIN=${skin}" "PLUGINS=${plugins}"
  render_template "$PATRABAHOK_HOME/templates/roundcube/password.config.inc.php.tmpl" "${release}/plugins/password/config.inc.php" \
    "PW_DB_PASSWORD=${ROUNDCUBE_PW_DB_PASSWORD}" "MAIL_DB_NAME=${mail_db_name}"
  chown root:www-data "${release}/config/config.inc.php" "${release}/plugins/password/config.inc.php"
  chmod 640 "${release}/config/config.inc.php" "${release}/plugins/password/config.inc.php"

  if [ -z "$(mysql -u root -N -B "$ROUNDCUBE_DB_NAME" -e "SHOW TABLES LIKE 'system';")" ]; then
    log_info "Initializing the Roundcube database schema..."
    mysql -u root "$ROUNDCUBE_DB_NAME" < "${release}/SQL/mysql.initial.sql"
  else
    log_info "Applying any pending Roundcube schema updates..."
    runuser -u www-data -- php "${release}/bin/updatedb.sh" --package=roundcube --dir="${release}/SQL" \
      || die "Roundcube schema update failed. See /var/log/roundcube/errors.log."
  fi

  render_template "$PATRABAHOK_HOME/templates/nginx/roundcube.conf.tmpl" /etc/nginx/sites-available/patrabahok-roundcube \
    "PORT=${ROUNDCUBE_PORT}" "ROUNDCUBE_ROOT=${ROUNDCUBE_ROOT}" "PHP_FPM_SOCK=${fpm_sock}"
  ln -sfn /etc/nginx/sites-available/patrabahok-roundcube /etc/nginx/sites-enabled/patrabahok-roundcube
  nginx -t >/dev/null 2>&1 || { nginx -t; die "nginx configuration test failed."; }

  cat > /etc/cron.d/patrabahok-roundcube <<EOF
# Purges deleted Roundcube records and expired sessions/caches.
17 3 * * * www-data php ${ROUNDCUBE_ROOT}/current/bin/cleandb.sh >/dev/null 2>&1
EOF
  cat > /etc/logrotate.d/patrabahok-roundcube <<'EOF'
/var/log/roundcube/*.log {
    weekly
    rotate 8
    compress
    delaycompress
    missingok
    notifempty
    create 0640 www-data www-data
}
EOF

  setup_managesieve

  systemctl enable "php${php_ver}-fpm" nginx >/dev/null 2>&1
  systemctl restart "php${php_ver}-fpm"
  systemctl restart nginx

  install -m 0644 "$PATRABAHOK_HOME/templates/fail2ban/jail-roundcube.local" /etc/fail2ban/jail.d/patrabahok-roundcube.local
  systemctl restart fail2ban || log_warn "fail2ban did not restart cleanly. Check 'fail2ban-client status'."

  state_set webmail_upstream "http://127.0.0.1:${ROUNDCUBE_PORT}"
  state_set webmail_built_version "${PATRABAHOK_INSTALLER_VERSION:-dev}"
  if [ "$flavor" = "patrabahok" ]; then
    log_ok "patrabahok Webmail installed (Roundcube ${ROUNDCUBE_VERSION} engine). Users sign in with their full email address."
  else
    log_ok "Roundcube ${ROUNDCUBE_VERSION} installed (users log in with their full email address and can change their password under Settings)."
  fi
}

remove_roundcube() {
  [ -e /etc/nginx/sites-enabled/patrabahok-roundcube ] || return 0
  log_info "Disabling Roundcube (another webmail was chosen). Its database and files are kept."
  rm -f /etc/nginx/sites-enabled/patrabahok-roundcube /etc/cron.d/patrabahok-roundcube \
    /etc/fail2ban/jail.d/patrabahok-roundcube.local
  systemctl disable --now nginx >/dev/null 2>&1 || true
  systemctl restart fail2ban >/dev/null 2>&1 || true
}

# --- patrabahok Mail (mail-client/) -------------------------------------------------

ensure_node() {
  if [ -x "${NODE_DIR}/bin/node" ] && [ "$("${NODE_DIR}/bin/node" --version)" = "v${NODE_VERSION}" ]; then
    return 0
  fi
  local arch sha256
  case "$(uname -m)" in
    x86_64) arch=x64; sha256="$NODE_SHA256_X64" ;;
    aarch64) arch=arm64; sha256="$NODE_SHA256_ARM64" ;;
    *) die "Unsupported CPU architecture for Node.js: $(uname -m)" ;;
  esac
  log_info "Downloading Node.js ${NODE_VERSION} (${arch})..."
  local tmp tarball="node-v${NODE_VERSION}-linux-${arch}.tar.xz"
  tmp="$(mktemp -d)"
  download_verified "https://nodejs.org/dist/v${NODE_VERSION}/${tarball}" "$sha256" "${tmp}/${tarball}"
  rm -rf "$NODE_DIR"
  mkdir -p "$NODE_DIR"
  tar -C "$NODE_DIR" --strip-components=1 -xJf "${tmp}/${tarball}" || die "Failed to extract Node.js."
  rm -rf "$tmp"
}

# ensure_base64_key NAME: the mail client's own keys (cli/internal/secretkey format:
# base64 of 32 random bytes). Generated here because its systemd sandbox can't write
# /etc/patrabahok/secrets.env itself.
ensure_base64_key() {
  local name="$1"
  secrets_init
  grep -q "^${name}=" "$PATRABAHOK_SECRETS_FILE" && return 0
  printf '%s=%s\n' "$name" "$(openssl rand -base64 32)" >> "$PATRABAHOK_SECRETS_FILE"
  chmod 600 "$PATRABAHOK_SECRETS_FILE"
}

install_mail_client() {
  local hostname
  hostname="$(state_get hostname)"
  [ -n "$hostname" ] || die "hostname missing from state. Run preflight first."
  [ -f "$PATRABAHOK_HOME/mail-client/package.json" ] || die "mail-client source not found at $PATRABAHOK_HOME/mail-client"

  log_info "Installing PostgreSQL for patrabahok Mail..."
  apt_install postgresql xz-utils curl
  systemctl enable --now postgresql >/dev/null 2>&1

  secret_ensure MAIL_CLIENT_DB_PASSWORD
  runuser -u postgres -- psql -v ON_ERROR_STOP=1 -q <<SQL
DO \$\$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '${MAIL_CLIENT_DB_USER}') THEN
    CREATE ROLE ${MAIL_CLIENT_DB_USER} LOGIN;
  END IF;
END
\$\$;
ALTER ROLE ${MAIL_CLIENT_DB_USER} PASSWORD '${MAIL_CLIENT_DB_PASSWORD}';
SQL
  if [ -z "$(runuser -u postgres -- psql -tAc "SELECT 1 FROM pg_database WHERE datname = '${MAIL_CLIENT_DB_NAME}'")" ]; then
    runuser -u postgres -- createdb -O "$MAIL_CLIENT_DB_USER" "$MAIL_CLIENT_DB_NAME"
  fi

  ensure_node
  ensure_base64_key MAIL_CLIENT_SESSION_KEY
  ensure_base64_key MAIL_CLIENT_ENC_KEY

  log_info "Building patrabahok Mail (this can take a few minutes)..."
  systemctl stop patrabahok-mail >/dev/null 2>&1 || true
  rm -rf "$MAIL_CLIENT_DIR"
  mkdir -p "$MAIL_CLIENT_DIR"
  cp -a "$PATRABAHOK_HOME/mail-client/." "$MAIL_CLIENT_DIR/"
  rm -rf "${MAIL_CLIENT_DIR}/node_modules" "${MAIL_CLIENT_DIR}/.next"

  local database_url="postgres://${MAIL_CLIENT_DB_USER}:${MAIL_CLIENT_DB_PASSWORD}@127.0.0.1:5432/${MAIL_CLIENT_DB_NAME}"
  umask 077
  cat > "${MAIL_CLIENT_DIR}/.env" <<EOF
NODE_ENV=production
DATABASE_URL=${database_url}
LISTEN_HOST=127.0.0.1
PORT=${MAIL_CLIENT_PORT}
PATRABAHOK_IMAP_HOST=${hostname}
PATRABAHOK_SMTP_HOST=${hostname}
EOF
  umask 022

  (
    cd "$MAIL_CLIENT_DIR"
    export PATH="${NODE_DIR}/bin:${PATH}" HOME="${HOME:-/root}" NEXT_TELEMETRY_DISABLED=1
    npm ci --no-audit --no-fund
    npm run build
    DATABASE_URL="$database_url" npx drizzle-kit migrate
  ) || die "Building patrabahok Mail failed. See the output above."

  render_template "$PATRABAHOK_HOME/templates/systemd/patrabahok-mail.service.tmpl" /etc/systemd/system/patrabahok-mail.service \
    "NODE_BIN=${NODE_DIR}/bin/node"
  systemctl daemon-reload
  systemctl enable patrabahok-mail >/dev/null 2>&1
  systemctl restart patrabahok-mail

  state_set webmail_upstream "http://127.0.0.1:${MAIL_CLIENT_PORT}"
  log_ok "patrabahok Mail installed."
}

remove_mail_client() {
  [ -f /etc/systemd/system/patrabahok-mail.service ] || return 0
  log_info "Disabling patrabahok Mail (another webmail was chosen). Its database is kept."
  systemctl disable --now patrabahok-mail >/dev/null 2>&1 || true
}

phase_run() {
  local webmail
  webmail="$(choose_webmail)"
  state_set webmail "$webmail"

  case "$webmail" in
    patrabahok|roundcube)
      remove_mail_client
      install_roundcube "$webmail"
      ;;
    custom)
      remove_roundcube
      install_mail_client
      ;;
    none)
      remove_roundcube
      remove_mail_client
      state_set webmail_upstream ""
      log_info "No webmail installed. Users connect with IMAP (993) / SMTP submission (587) mail apps."
      ;;
  esac

  # On a fresh install 95-cli writes patrabahokd's unit next. When this phase re-runs
  # later (an upgrade, or a different choice), refresh the already-installed unit so the
  # public :443 listener proxies to the right upstream.
  if ensure_current_patrabahokd; then
    render_patrabahokd_unit
    systemctl daemon-reload
    systemctl restart patrabahokd
  fi

  local upstream
  upstream="$(state_get webmail_upstream)"
  [ -n "$upstream" ] && log_ok "Webmail: https://$(state_get hostname)/ (proxied to ${upstream})"
  return 0
}

phase_run
