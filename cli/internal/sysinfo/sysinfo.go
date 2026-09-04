// Package sysinfo reads installer-produced files and shells out to postqueue for
// operations that aren't backed by the database: DKIM/DNS record text, the mail
// queue, and installer state.
package sysinfo

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	DKIMDir    = "/var/lib/rspamd/dkim"
	Selector   = "mail"
	StateFile  = "/etc/patrabahok/state.json"
	DNSDumpDir = "/root"
)

func DKIMRecord(domain string) (string, error) {
	path := filepath.Join(DKIMDir, domain+"."+Selector+".txt")
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("no DKIM record found for %s (expected %s): %w", domain, path, err)
	}
	return string(b), nil
}

func DNSRecords(domain string) (string, error) {
	path := filepath.Join(DNSDumpDir, "patrabahok-dns-"+domain+".txt")
	b, err := os.ReadFile(path)
	if err == nil {
		return string(b), nil
	}
	// Fall back to just the DKIM record if the full dump isn't present.
	return DKIMRecord(domain)
}

func QueueList() (string, error) {
	out, err := exec.Command("postqueue", "-p").CombinedOutput()
	return string(out), err
}

func QueueFlush() (string, error) {
	out, err := exec.Command("postqueue", "-f").CombinedOutput()
	return string(out), err
}

func InstallerState() (map[string]any, error) {
	b, err := os.ReadFile(StateFile)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", StateFile, err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", StateFile, err)
	}
	return m, nil
}
