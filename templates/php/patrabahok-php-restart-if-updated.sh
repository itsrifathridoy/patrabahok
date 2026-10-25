#!/bin/sh
# Managed by patrabahok (lib/phases/90-webmail.sh). Run by apt after every dpkg run
# (/etc/apt/apt.conf.d/99patrabahok-php-restart): restarts PHP-FPM when its binary was
# replaced after the service started, so a PHP security update (unattended or manual)
# takes effect immediately instead of at the next reboot.
command -v systemctl >/dev/null 2>&1 || exit 0
[ -d /run/systemd/system ] || exit 0
for unit in $(systemctl list-units --type=service --state=running --no-legend --plain 'php*-fpm.service' 2>/dev/null | awk '{print $1}'); do
  ver="${unit#php}"
  ver="${ver%-fpm.service}"
  bin="/usr/sbin/php-fpm${ver}"
  [ -x "$bin" ] || continue
  started="$(systemctl show "$unit" -p ActiveEnterTimestamp --value 2>/dev/null)"
  [ -n "$started" ] || continue
  started="$(date -d "$started" +%s 2>/dev/null)" || continue
  if [ "$(stat -c %Y "$bin")" -gt "$started" ]; then
    logger -t patrabahok "PHP-FPM binary updated; restarting $unit"
    systemctl restart "$unit" || true
  fi
done
exit 0
