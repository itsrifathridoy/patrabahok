// Package ops is the dashboard's control surface for the server itself: the services
// the mail stack runs on, fail2ban, the Postfix queue, logs, and spam/antivirus stats.
// Every operation runs a fixed command with validated arguments (no shell, no
// free-form input reaches a command line), and only the actions listed per service in
// the catalog are possible.
package ops

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Service actions.
const (
	Start   = "start"
	Stop    = "stop"
	Restart = "restart"
	Reload  = "reload"
)

// ServiceDef describes one managed service and what may be done to it from the
// dashboard.
type ServiceDef struct {
	Unit        string
	Label       string
	Description string
	Actions     []string
	// Async services are acted on with --no-block: restarting them interrupts the very
	// request asking for it (patrabahokd serves the dashboard; the dashboard needs
	// MariaDB), so waiting for the result would only produce a broken response.
	Async bool
	// Warning is shown before acting, for actions with real side effects.
	Warning string
}

func (d ServiceDef) Allows(action string) bool {
	for _, a := range d.Actions {
		if a == action {
			return true
		}
	}
	return false
}

// Catalog returns the managed services. Webmail services are included only for the
// webmail chosen at install time.
func Catalog(webmail string) []ServiceDef {
	all := []ServiceDef{
		{Unit: "postfix", Label: "Postfix", Description: "Sends and receives mail (SMTP)", Actions: []string{Start, Stop, Restart, Reload},
			Warning: "Stopping Postfix stops all mail delivery. Senders retry later, so nothing is lost while it's briefly down."},
		{Unit: "dovecot", Label: "Dovecot", Description: "Mailbox access (IMAP) and local delivery", Actions: []string{Start, Stop, Restart, Reload},
			Warning: "Restarting Dovecot disconnects everyone's mail apps for a moment; they reconnect automatically."},
		{Unit: "rspamd", Label: "Rspamd", Description: "Spam filtering, DKIM signing and DMARC", Actions: []string{Start, Stop, Restart, Reload},
			Warning: "While Rspamd is stopped, mail is accepted without spam filtering or DKIM signing."},
		{Unit: "clamav-daemon", Label: "ClamAV", Description: "Virus scanning of incoming mail", Actions: []string{Start, Stop, Restart},
			Warning: "ClamAV takes about a minute and a lot of memory to load its signatures after a restart."},
		{Unit: "clamav-freshclam", Label: "Freshclam", Description: "Keeps virus signatures up to date", Actions: []string{Start, Stop, Restart}},
		{Unit: "redis-server", Label: "Redis", Description: "Rspamd's statistics and rate-limit store", Actions: []string{Restart}},
		{Unit: "unbound", Label: "Unbound", Description: "Local DNS resolver for spam blocklist lookups", Actions: []string{Restart, Reload}},
		{Unit: "fail2ban", Label: "Fail2ban", Description: "Bans IPs after repeated failed logins", Actions: []string{Restart, Reload},
			Warning: "Restarting fail2ban keeps current bans (they are stored on disk)."},
		{Unit: "mariadb", Label: "MariaDB", Description: "Users, groups, domains and settings database", Actions: []string{Restart}, Async: true,
			Warning: "Mail lookups and this dashboard pause for a few seconds while MariaDB restarts."},
		{Unit: "patrabahokd", Label: "patrabahokd", Description: "This dashboard, the APIs and group posting rules", Actions: []string{Restart}, Async: true,
			Warning: "The dashboard is unavailable for a few seconds while it restarts. Reload the page afterwards."},
	}
	switch webmail {
	case "patrabahok", "roundcube":
		all = append(all,
			ServiceDef{Unit: "nginx", Label: "nginx", Description: "Serves Roundcube webmail (loopback only)", Actions: []string{Restart, Reload}},
			ServiceDef{Unit: phpFPMUnit(), Label: "PHP-FPM", Description: "Runs Roundcube webmail", Actions: []string{Restart, Reload}})
	case "custom":
		all = append(all,
			ServiceDef{Unit: "patrabahok-mail", Label: "patrabahok Mail", Description: "Webmail client", Actions: []string{Start, Stop, Restart}},
			ServiceDef{Unit: "postgresql", Label: "PostgreSQL", Description: "Webmail cache database", Actions: []string{Restart}})
	}
	return all
}

// phpFPMUnit finds the installed php<version>-fpm unit name.
func phpFPMUnit() string {
	out, err := exec.Command("systemctl", "list-unit-files", "--no-legend", "php*-fpm.service").Output()
	if err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if f := strings.Fields(line); len(f) > 0 {
				return strings.TrimSuffix(f[0], ".service")
			}
		}
	}
	return "php-fpm"
}

func Find(webmail, unit string) (ServiceDef, bool) {
	for _, d := range Catalog(webmail) {
		if d.Unit == unit {
			return d, true
		}
	}
	return ServiceDef{}, false
}

// ServiceState is a service's live status.
type ServiceState struct {
	ServiceDef
	Active      string // active, inactive, failed, activating, ...
	SubState    string
	Since       time.Time
	MemoryBytes int64
	Enabled     bool
}

func (s ServiceState) Running() bool { return s.Active == "active" }

// States returns the live status of every managed service, from one systemctl call.
func States(webmail string) []ServiceState {
	defs := Catalog(webmail)
	args := []string{"show", "--property=Id,ActiveState,SubState,ActiveEnterTimestamp,MemoryCurrent,UnitFileState"}
	for _, d := range defs {
		args = append(args, d.Unit+".service")
	}
	out, _ := exec.Command("systemctl", args...).Output()
	byUnit := map[string]map[string]string{}
	for _, block := range strings.Split(string(out), "\n\n") {
		props := map[string]string{}
		for _, line := range strings.Split(block, "\n") {
			if k, v, ok := strings.Cut(line, "="); ok {
				props[k] = v
			}
		}
		if id := props["Id"]; id != "" {
			byUnit[strings.TrimSuffix(id, ".service")] = props
		}
	}
	states := make([]ServiceState, 0, len(defs))
	for _, d := range defs {
		p := byUnit[d.Unit]
		st := ServiceState{ServiceDef: d, Active: p["ActiveState"], SubState: p["SubState"], Enabled: p["UnitFileState"] == "enabled"}
		if st.Active == "" {
			st.Active = "unknown"
		}
		if t, err := time.Parse("Mon 2006-01-02 15:04:05 MST", p["ActiveEnterTimestamp"]); err == nil {
			st.Since = t
		}
		if m, err := strconv.ParseInt(p["MemoryCurrent"], 10, 64); err == nil && m > 0 && m < 1<<50 {
			st.MemoryBytes = m
		}
		states = append(states, st)
	}
	return states
}

var ErrNotAllowed = errors.New("that action isn't allowed for this service")

// Control runs action on a cataloged service and reports systemd's error output, if any.
func Control(ctx context.Context, webmail, unit, action string) error {
	def, ok := Find(webmail, unit)
	if !ok || !def.Allows(action) {
		return ErrNotAllowed
	}
	args := []string{action}
	if def.Async {
		args = append(args, "--no-block")
	}
	args = append(args, def.Unit+".service")
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("systemctl %s %s failed: %s", action, def.Unit, msg)
	}
	return nil
}

// ServiceLogs returns the last n journal lines for a cataloged service.
func ServiceLogs(webmail, unit string, n int) (string, error) {
	def, ok := Find(webmail, unit)
	if !ok {
		return "", ErrNotAllowed
	}
	n = clampLines(n)
	out, err := exec.Command("journalctl", "-u", def.Unit+".service", "-n", strconv.Itoa(n), "--no-pager", "-o", "short-iso").CombinedOutput()
	if err != nil && len(out) == 0 {
		return "", err
	}
	return string(out), nil
}

func clampLines(n int) int {
	switch {
	case n <= 0:
		return 200
	case n > 2000:
		return 2000
	}
	return n
}
