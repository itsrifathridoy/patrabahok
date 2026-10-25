#!/usr/bin/env bash
# apply_mail_routing — renders the alias map (aliases, groups, forwarding, catch-all) and
# wires patrabahokd's policy service into Postfix's recipient restrictions. Called by
# phase 50-postfix on install and by 55-mail-routing on upgrade, so both end up with
# exactly the same settings. Requires log.sh, state.sh, secrets.sh (loaded) and
# template.sh.

PATRABAHOK_POLICY_ADDR="127.0.0.1:10040"

apply_mail_routing() {
  local db_name db_readonly_user
  db_name="$(state_get db_name)"
  db_readonly_user="$(state_get db_readonly_user)"
  [ -n "$db_name" ] && [ -n "$db_readonly_user" ] || die "db_name/db_readonly_user missing from state. Run phase 30-database first."

  render_template "$PATRABAHOK_HOME/templates/postfix/mysql-virtual-alias-maps.cf.tmpl" \
    /etc/postfix/mysql-virtual-alias-maps.cf \
    "DB_READONLY_USER=${db_readonly_user}" "DB_READONLY_PASSWORD=${MAIL_DB_READONLY_PASSWORD}" "DB_NAME=${db_name}"
  chown root:postfix /etc/postfix/mysql-virtual-alias-maps.cf
  chmod 640 /etc/postfix/mysql-virtual-alias-maps.cf

  local policy="check_policy_service inet:${PATRABAHOK_POLICY_ADDR}"
  # The policy check comes first so authenticated users are subject to group posting
  # policies too. It answers DUNNO for anything that isn't a group.
  postconf -e "smtpd_recipient_restrictions=${policy}, permit_mynetworks, permit_sasl_authenticated, reject_unauth_destination, reject_unknown_recipient_domain, reject_unlisted_recipient"
  # master.cf "-o" values can't contain whitespace (postconf -P refuses them), and the
  # policy check has one. So the list lives in a named main.cf parameter that both
  # authenticated-submission services reference.
  postconf -e "patrabahok_submission_recipient_restrictions=${policy}, permit_sasl_authenticated, reject"
  postconf -P "submission/inet/smtpd_recipient_restrictions=\$patrabahok_submission_recipient_restrictions"
  postconf -P "smtps/inet/smtpd_recipient_restrictions=\$patrabahok_submission_recipient_restrictions"
  # If the policy service is unreachable, defer (the sender retries) rather than
  # silently letting restricted posts through.
  postconf -e "smtpd_policy_service_default_action=451 4.3.5 Group policy service unavailable, try again later"
  # Every supported OS ships Postfix 3.6 or newer; a lower level makes each Postfix
  # command warn about "backwards-compatible default settings".
  postconf -e "compatibility_level=3.6"
  # Postfix's default of 1000 addresses per expansion is smaller than a school-wide group.
  postconf -e "virtual_alias_expansion_limit=20000"
}
