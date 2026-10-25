package ops

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// SpamStats are Rspamd's counters since its statistics were last reset.
type SpamStats struct {
	Available bool
	Scanned   int64
	Actions   []ActionCount // reject, add header, greylist, no action, ...
	Learned   int64
}

type ActionCount struct {
	Action  string
	Count   int64
	Percent float64
}

var (
	scannedRe = regexp.MustCompile(`(?m)^Messages scanned:\s*(\d+)`)
	learnedRe = regexp.MustCompile(`(?m)^Messages learned:\s*(\d+)`)
	actionRe  = regexp.MustCompile(`(?m)^Messages with action ([^:]+):\s*(\d+),\s*([\d.]+)%`)
)

// Spam reads `rspamc stat` (Rspamd's controller on localhost).
func Spam() SpamStats {
	out, err := exec.Command("rspamc", "stat").Output()
	if err != nil {
		return SpamStats{}
	}
	s := string(out)
	st := SpamStats{Available: true}
	if m := scannedRe.FindStringSubmatch(s); m != nil {
		st.Scanned, _ = strconv.ParseInt(m[1], 10, 64)
	}
	if m := learnedRe.FindStringSubmatch(s); m != nil {
		st.Learned, _ = strconv.ParseInt(m[1], 10, 64)
	}
	for _, m := range actionRe.FindAllStringSubmatch(s, -1) {
		n, _ := strconv.ParseInt(m[2], 10, 64)
		p, _ := strconv.ParseFloat(m[3], 64)
		st.Actions = append(st.Actions, ActionCount{Action: strings.TrimSpace(m[1]), Count: n, Percent: p})
	}
	return st
}

// AntivirusInfo is ClamAV's engine and signature database version.
type AntivirusInfo struct {
	Available bool
	Engine    string
	Signature string // database version number
	Updated   string // database date
}

// Antivirus parses `clamdscan --version` ("ClamAV 1.0.7/27440/Thu Oct 23 08:30:00 2026"),
// which asks the running daemon, so it reflects the signatures actually loaded.
func Antivirus() AntivirusInfo {
	out, err := exec.Command("clamdscan", "--version").Output()
	if err != nil {
		out, err = exec.Command("clamscan", "--version").Output()
		if err != nil {
			return AntivirusInfo{}
		}
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), "/", 3)
	info := AntivirusInfo{Available: true, Engine: strings.TrimPrefix(parts[0], "ClamAV ")}
	if len(parts) > 1 {
		info.Signature = parts[1]
	}
	if len(parts) > 2 {
		info.Updated = parts[2]
	}
	return info
}
