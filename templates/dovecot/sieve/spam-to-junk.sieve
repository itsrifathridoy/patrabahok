# Global "before" sieve script, applied to every mailbox ahead of any personal sieve
# rules. Files mail Rspamd scored as spam (its "add header" action — see
# templates/rspamd/local.d/milter_headers.conf.tmpl's extended_spam_headers, which adds
# X-Rspamd-Action) into Junk instead of leaving it in INBOX. Outright rejects (Rspamd's
# "reject" action) never reach here at all — Postfix refuses them at SMTP time.
require ["fileinto", "mailbox"];

if header :contains "X-Rspamd-Action" "add header" {
    fileinto :create "Junk";
    stop;
}
