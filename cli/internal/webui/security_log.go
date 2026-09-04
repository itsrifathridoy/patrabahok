package webui

import (
	"log"
	"log/syslog"
	"net"
	"sync"
)

// authLog sends failed-login notices to the system's authpriv syslog facility, so
// they land in /var/log/auth.log via the same rsyslog path SSH/Postfix/Dovecot use —
// letting fail2ban's patrabahok-dashboard jail (see templates/fail2ban) watch it the
// same way it watches everything else, rather than inventing a separate mechanism.
var (
	authLogOnce sync.Once
	authLogger  *syslog.Writer
)

func logFailedLogin(username, remoteAddr string) {
	authLogOnce.Do(func() {
		w, err := syslog.New(syslog.LOG_AUTHPRIV|syslog.LOG_NOTICE, "patrabahokd")
		if err != nil {
			log.Printf("webui: could not connect to syslog for auth logging: %v", err)
			return
		}
		authLogger = w
	})
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	msg := "Failed dashboard login for user \"" + username + "\" from " + host
	if authLogger != nil {
		_ = authLogger.Notice(msg)
	} else {
		log.Print(msg)
	}
}
