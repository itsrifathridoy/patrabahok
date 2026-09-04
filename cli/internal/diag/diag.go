// Package diag gathers the same signals a human would check by hand when something
// seems wrong: service status, config validation, certificate expiry, disk space,
// recent log errors, and current fail2ban bans.
package diag

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

type ServiceStatus struct {
	Name   string
	Active bool
}

var Services = []string{
	"postfix", "dovecot", "rspamd", "redis-server", "clamav-daemon",
	"fail2ban", "unbound", "mariadb", "patrabahokd",
}

func CheckServices() []ServiceStatus {
	out := make([]ServiceStatus, 0, len(Services))
	for _, s := range Services {
		active := exec.Command("systemctl", "is-active", "--quiet", s).Run() == nil
		out = append(out, ServiceStatus{Name: s, Active: active})
	}
	return out
}

type ConfigCheck struct {
	Name   string
	Output string
	Pass   bool
}

func CheckConfigs() []ConfigCheck {
	checks := []struct {
		name string
		bin  string
		args []string
	}{
		{"postfix check", "postfix", []string{"check"}},
		{"doveconf -n", "doveconf", []string{"-n"}},
		{"rspamd configtest", "rspamadm", []string{"configtest"}},
	}
	out := make([]ConfigCheck, 0, len(checks))
	for _, c := range checks {
		b, err := exec.Command(c.bin, c.args...).CombinedOutput()
		out = append(out, ConfigCheck{Name: c.name, Output: strings.TrimSpace(string(b)), Pass: err == nil})
	}
	return out
}

type TLSInfo struct {
	CommonName string
	NotAfter   time.Time
	DaysLeft   int
	Valid      bool
	Error      string
}

func CheckTLS(certPath string) TLSInfo {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return TLSInfo{Error: err.Error()}
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return TLSInfo{Error: "could not parse certificate PEM"}
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return TLSInfo{Error: err.Error()}
	}
	return TLSInfo{
		CommonName: cert.Subject.CommonName,
		NotAfter:   cert.NotAfter,
		DaysLeft:   int(time.Until(cert.NotAfter).Hours() / 24),
		Valid:      time.Now().Before(cert.NotAfter),
	}
}

type DiskUsage struct {
	Path        string
	TotalBytes  uint64
	FreeBytes   uint64
	UsedPercent float64
}

func CheckDisk(path string) (DiskUsage, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return DiskUsage{}, err
	}
	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bavail * uint64(stat.Bsize)
	used := total - free
	pct := 0.0
	if total > 0 {
		pct = float64(used) / float64(total) * 100
	}
	return DiskUsage{Path: path, TotalBytes: total, FreeBytes: free, UsedPercent: pct}, nil
}

// RecentIssues returns the most recent lines from logPath matching common problem
// keywords, newest last. Reads only the tail of the file (via the `tail` binary) to
// stay cheap regardless of how large the log has grown.
func RecentIssues(logPath string, maxLines int) ([]string, error) {
	out, err := exec.Command("tail", "-n", "3000", logPath).Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(out), "\n")
	var matches []string
	for i := len(lines) - 1; i >= 0 && len(matches) < maxLines; i-- {
		l := strings.ToLower(lines[i])
		if strings.Contains(l, "reject") || strings.Contains(l, "bounce") ||
			strings.Contains(l, "error") || strings.Contains(l, "warning") ||
			strings.Contains(l, "fatal") {
			matches = append(matches, lines[i])
		}
	}
	for i, j := 0, len(matches)-1; i < j; i, j = i+1, j-1 {
		matches[i], matches[j] = matches[j], matches[i]
	}
	return matches, nil
}

type BannedIP struct {
	Jail string
	IP   string
}

func Fail2banBans() ([]BannedIP, error) {
	out, err := exec.Command("fail2ban-client", "status").CombinedOutput()
	if err != nil {
		return nil, err
	}
	var jails []string
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "Jail list") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		for _, j := range strings.Split(parts[1], ",") {
			if j = strings.TrimSpace(j); j != "" {
				jails = append(jails, j)
			}
		}
	}

	var bans []BannedIP
	for _, jail := range jails {
		out, err := exec.Command("fail2ban-client", "status", jail).CombinedOutput()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(out), "\n") {
			if !strings.Contains(line, "Banned IP list") {
				continue
			}
			parts := strings.SplitN(line, ":", 2)
			if len(parts) != 2 {
				continue
			}
			for _, ip := range strings.Fields(parts[1]) {
				bans = append(bans, BannedIP{Jail: jail, IP: ip})
			}
		}
	}
	return bans, nil
}
