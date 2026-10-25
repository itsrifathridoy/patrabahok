package ops

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// LogSource is a log the dashboard may show. Sources are fixed: a request can only
// pick one by key, never name a path or unit itself.
type LogSource struct {
	Key, Label, Description string
	File                    string // read with tail, or
	Unit                    string // read from the journal
}

var logSources = []LogSource{
	{Key: "mail", Label: "Mail", Description: "Postfix, Dovecot and Rspamd: deliveries, rejections, sign-ins", File: "/var/log/mail.log"},
	{Key: "auth", Label: "Authentication", Description: "SSH and dashboard sign-in attempts", File: "/var/log/auth.log"},
	{Key: "fail2ban", Label: "Fail2ban", Description: "Bans and unbans", File: "/var/log/fail2ban.log"},
	{Key: "rspamd", Label: "Rspamd", Description: "Spam filter decisions and DKIM signing", File: "/var/log/rspamd/rspamd.log"},
	{Key: "webmail", Label: "Webmail", Description: "Roundcube errors and failed sign-ins", File: "/var/log/roundcube/errors.log"},
	{Key: "patrabahokd", Label: "patrabahokd", Description: "This dashboard, the APIs and group posting decisions", Unit: "patrabahokd"},
}

func LogSources() []LogSource {
	out := []LogSource{}
	for _, s := range logSources {
		if s.File != "" {
			if _, err := os.Stat(s.File); err != nil {
				continue
			}
		}
		out = append(out, s)
	}
	return out
}

func findSource(key string) (LogSource, bool) {
	for _, s := range LogSources() {
		if s.Key == key {
			return s, true
		}
	}
	return LogSource{}, false
}

// TailLog returns up to n of the most recent lines from a source, newest last. With a
// filter, it scans further back (up to 20,000 lines) and keeps the n most recent lines
// containing it, case-insensitively.
func TailLog(key string, n int, filter string) ([]string, error) {
	src, ok := findSource(key)
	if !ok {
		return nil, ErrNotAllowed
	}
	n = clampLines(n)
	scan := n
	if filter != "" {
		scan = 20000
	}
	var out []byte
	var err error
	if src.File != "" {
		out, err = exec.Command("tail", "-n", strconv.Itoa(scan), src.File).Output()
	} else {
		out, err = exec.Command("journalctl", "-u", src.Unit+".service", "-n", strconv.Itoa(scan), "--no-pager", "-o", "short-iso").Output()
	}
	if err != nil && len(out) == 0 {
		return nil, err
	}
	filter = strings.ToLower(strings.TrimSpace(filter))
	var lines []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if filter == "" || strings.Contains(strings.ToLower(line), filter) {
			lines = append(lines, line)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}

// LineLevel classifies a log line for coloring: "error", "warn" or "".
func LineLevel(line string) string {
	l := strings.ToLower(line)
	switch {
	case strings.Contains(l, "error") || strings.Contains(l, "fatal") || strings.Contains(l, "panic") ||
		strings.Contains(l, "reject") || strings.Contains(l, " ban ") || strings.Contains(l, "auth failed") || strings.Contains(l, "authentication failed"):
		return "error"
	case strings.Contains(l, "warn") || strings.Contains(l, "deferred") || strings.Contains(l, "failed"):
		return "warn"
	}
	return ""
}
