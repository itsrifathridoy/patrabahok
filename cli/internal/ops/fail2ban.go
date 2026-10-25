package ops

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Jail is one fail2ban jail's live status.
type Jail struct {
	Name            string
	CurrentlyFailed int
	TotalFailed     int
	CurrentlyBanned int
	TotalBanned     int
	BannedIPs       []string
}

var jailNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

func fail2ban(args ...string) (string, error) {
	out, err := exec.Command("fail2ban-client", args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("fail2ban: %s", msg)
	}
	return string(out), nil
}

// JailNames lists the active jails.
func JailNames() ([]string, error) {
	out, err := fail2ban("status")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(out, "\n") {
		if _, list, ok := strings.Cut(line, "Jail list:"); ok {
			for _, j := range strings.Split(list, ",") {
				if j = strings.TrimSpace(j); j != "" {
					names = append(names, j)
				}
			}
		}
	}
	sort.Strings(names)
	return names, nil
}

// Jails returns every active jail's counters and banned IPs.
func Jails() ([]Jail, error) {
	names, err := JailNames()
	if err != nil {
		return nil, err
	}
	jails := make([]Jail, 0, len(names))
	for _, name := range names {
		out, err := fail2ban("status", name)
		if err != nil {
			continue
		}
		j := Jail{Name: name}
		for _, line := range strings.Split(out, "\n") {
			key, val, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			key = strings.Trim(key, " |`-\t")
			val = strings.TrimSpace(val)
			n, _ := strconv.Atoi(val)
			switch key {
			case "Currently failed":
				j.CurrentlyFailed = n
			case "Total failed":
				j.TotalFailed = n
			case "Currently banned":
				j.CurrentlyBanned = n
			case "Total banned":
				j.TotalBanned = n
			case "Banned IP list":
				j.BannedIPs = strings.Fields(val)
			}
		}
		jails = append(jails, j)
	}
	return jails, nil
}

// ValidateIP accepts a single IPv4/IPv6 address.
func ValidateIP(s string) (string, error) {
	ip := net.ParseIP(strings.TrimSpace(s))
	if ip == nil {
		return "", fmt.Errorf("%q is not a valid IP address", s)
	}
	return ip.String(), nil
}

// Unban removes ip from every jail (jail == "") or from one jail.
func Unban(ip, jail string) error {
	ip, err := ValidateIP(ip)
	if err != nil {
		return err
	}
	if jail == "" {
		_, err = fail2ban("unban", ip)
		return err
	}
	if !jailNameRe.MatchString(jail) {
		return errors.New("invalid jail name")
	}
	_, err = fail2ban("set", jail, "unbanip", ip)
	return err
}

// Ban bans ip in the given jail, using that jail's ban time and firewall action.
func Ban(ip, jail string) error {
	ip, err := ValidateIP(ip)
	if err != nil {
		return err
	}
	names, err := JailNames()
	if err != nil {
		return err
	}
	found := false
	for _, n := range names {
		found = found || n == jail
	}
	if !found {
		return fmt.Errorf("no active jail named %q", jail)
	}
	_, err = fail2ban("set", jail, "banip", ip)
	return err
}

// --- allowlist ---------------------------------------------------------------------

// AllowlistFile holds the dashboard-managed fail2ban ignoreip list. jail.d/*.local is
// read after jail.local, so its [DEFAULT] ignoreip applies to every jail.
const AllowlistFile = "/etc/fail2ban/jail.d/patrabahok-allowlist.local"

// Always-present loopback entries; never shown as removable.
var builtinAllow = []string{"127.0.0.1/8", "::1"}

// Allowlist returns the dashboard-managed never-ban addresses and networks.
func Allowlist() ([]string, error) {
	b, err := os.ReadFile(AllowlistFile)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		if _, val, ok := strings.Cut(line, "ignoreip"); ok {
			val = strings.TrimLeft(strings.TrimSpace(val), "= ")
			for _, e := range strings.Fields(val) {
				if !isBuiltin(e) {
					out = append(out, e)
				}
			}
		}
	}
	return out, nil
}

func isBuiltin(e string) bool {
	for _, b := range builtinAllow {
		if b == e {
			return true
		}
	}
	return false
}

// ValidateAllowEntry accepts an IP or a CIDR network no wider than /16 (IPv4) or /32
// (IPv6): allowlisting a huge range would quietly switch brute-force protection off
// for a large part of the internet.
func ValidateAllowEntry(s string) (string, error) {
	s = strings.TrimSpace(s)
	if ip := net.ParseIP(s); ip != nil {
		return ip.String(), nil
	}
	_, network, err := net.ParseCIDR(s)
	if err != nil {
		return "", fmt.Errorf("%q is not an IP address or CIDR network (e.g. 203.0.113.7 or 203.0.113.0/24)", s)
	}
	ones, bits := network.Mask.Size()
	if (bits == 32 && ones < 16) || (bits == 128 && ones < 32) {
		return "", fmt.Errorf("%s is too broad to allowlist (use /16 or narrower for IPv4, /32 or narrower for IPv6)", network)
	}
	return network.String(), nil
}

// SetAllowlist replaces the managed allowlist and reloads fail2ban so it takes effect.
func SetAllowlist(entries []string) error {
	seen := map[string]bool{}
	var clean []string
	for _, e := range entries {
		v, err := ValidateAllowEntry(e)
		if err != nil {
			return err
		}
		if !seen[v] && !isBuiltin(v) {
			seen[v] = true
			clean = append(clean, v)
		}
	}
	content := "# Managed by the patrabahok dashboard (Security → Firewall & bans). Edits here are overwritten.\n" +
		"[DEFAULT]\nignoreip = " + strings.Join(append(append([]string{}, builtinAllow...), clean...), " ") + "\n"
	tmp := AllowlistFile + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, AllowlistFile); err != nil {
		return err
	}
	_, err := fail2ban("reload")
	return err
}

// IsBanned reports the jails that currently ban ip.
func IsBanned(ip string, jails []Jail) []string {
	var in []string
	for _, j := range jails {
		for _, b := range j.BannedIPs {
			if b == ip {
				in = append(in, j.Name)
			}
		}
	}
	return in
}
