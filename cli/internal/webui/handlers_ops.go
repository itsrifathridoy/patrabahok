package webui

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/itsrifathridoy/patrabahok/cli/internal/ops"
	"github.com/itsrifathridoy/patrabahok/cli/internal/sysinfo"
)

// stateValue reads one installer config value (e.g. "webmail") from state.json.
func stateValue(key string) string {
	st, err := sysinfo.InstallerState()
	if err != nil {
		return ""
	}
	cfg, _ := st["config"].(map[string]any)
	v, _ := cfg[key].(string)
	return v
}

// clientIP is the address the request came from. The dashboard terminates TLS itself
// (no proxy in front), so RemoteAddr is the real client.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// --- Firewall & bans ----------------------------------------------------------------

type BansPageData struct {
	Base
	Jails       []ops.Jail
	JailNames   []string
	Allowlist   []string
	ClientIP    string
	ClientBans  []string // jails currently banning ClientIP
	TotalBanned int
	TotalFailed int
	Error       string
}

func (s *Server) handleBansPage(w http.ResponseWriter, r *http.Request) {
	data := BansPageData{
		Base:     s.base(w, r, "Firewall & bans", "bans", Crumb{Label: "Security"}, Crumb{Label: "Firewall & bans"}),
		ClientIP: clientIP(r),
	}
	jails, err := ops.Jails()
	if err != nil {
		data.Error = err.Error()
	}
	data.Jails = jails
	for _, j := range jails {
		data.JailNames = append(data.JailNames, j.Name)
		data.TotalBanned += j.CurrentlyBanned
		data.TotalFailed += j.CurrentlyFailed
	}
	data.ClientBans = ops.IsBanned(data.ClientIP, jails)
	data.Allowlist, _ = ops.Allowlist()
	renderPage(w, "bans", data)
}

func (s *Server) handleUnban(w http.ResponseWriter, r *http.Request) {
	ip, jail := formValue(r, "ip"), formValue(r, "jail")
	if err := ops.Unban(ip, jail); err != nil {
		fail(w, r, http.StatusBadRequest, err.Error())
		return
	}
	where := "every jail"
	if jail != "" {
		where = jail
	}
	s.record(r, "fail2ban.unban", ip, where)
	done(w, r, "/security/bans", ip+" unbanned from "+where+".")
}

func (s *Server) handleBan(w http.ResponseWriter, r *http.Request) {
	ip, jail := formValue(r, "ip"), formValue(r, "jail")
	norm, err := ops.ValidateIP(ip)
	if err != nil {
		fail(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if norm == clientIP(r) {
		fail(w, r, http.StatusBadRequest, "That's the IP you're connected from. Banning it would lock you out of this dashboard.")
		return
	}
	if err := ops.Ban(norm, jail); err != nil {
		fail(w, r, http.StatusBadRequest, err.Error())
		return
	}
	s.record(r, "fail2ban.ban", norm, jail)
	done(w, r, "/security/bans", norm+" banned in "+jail+".")
}

func (s *Server) handleAllowlistAdd(w http.ResponseWriter, r *http.Request) {
	entry, err := ops.ValidateAllowEntry(formValue(r, "entry"))
	if err != nil {
		fail(w, r, http.StatusBadRequest, err.Error())
		return
	}
	list, err := ops.Allowlist()
	if err != nil {
		failErr(w, r, err)
		return
	}
	if err := ops.SetAllowlist(append(list, entry)); err != nil {
		fail(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	// An allowlisted address that is already banned stays banned until unbanned.
	if net.ParseIP(entry) != nil {
		_ = ops.Unban(entry, "")
	}
	s.record(r, "fail2ban.allow.add", entry, "")
	done(w, r, "/security/bans", entry+" will never be banned.")
}

func (s *Server) handleAllowlistRemove(w http.ResponseWriter, r *http.Request) {
	entry := r.URL.Query().Get("entry")
	list, err := ops.Allowlist()
	if err != nil {
		failErr(w, r, err)
		return
	}
	var keep []string
	for _, e := range list {
		if e != entry {
			keep = append(keep, e)
		}
	}
	if err := ops.SetAllowlist(keep); err != nil {
		fail(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.record(r, "fail2ban.allow.remove", entry, "")
	done(w, r, "/security/bans", entry+" removed from the allowlist.")
}

// --- Services -----------------------------------------------------------------------

type ServicesPageData struct {
	Base
	Services []ops.ServiceState
	Running  int
	Spam     ops.SpamStats
	AV       ops.AntivirusInfo
}

func (s *Server) handleServicesPage(w http.ResponseWriter, r *http.Request) {
	data := ServicesPageData{
		Base:     s.base(w, r, "Services", "services", Crumb{Label: "System"}, Crumb{Label: "Services"}),
		Services: ops.States(stateValue("webmail")),
		Spam:     ops.Spam(),
		AV:       ops.Antivirus(),
	}
	for _, sv := range data.Services {
		if sv.Running() {
			data.Running++
		}
	}
	renderPage(w, "services", data)
}

func (s *Server) handleServiceAction(w http.ResponseWriter, r *http.Request) {
	unit, action := r.PathValue("unit"), r.PathValue("action")
	def, ok := ops.Find(stateValue("webmail"), unit)
	if !ok || !def.Allows(action) {
		fail(w, r, http.StatusBadRequest, "That action isn't available for this service.")
		return
	}
	// Recorded before acting: restarting patrabahokd or MariaDB can end this request.
	s.record(r, "service."+action, unit, "")
	if err := ops.Control(r.Context(), stateValue("webmail"), unit, action); err != nil {
		fail(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	msg := def.Label + " " + pastTense(action) + "."
	if def.Async {
		msg = def.Label + " is restarting. Give it a few seconds, then reload."
	}
	done(w, r, "/services", msg)
}

func pastTense(action string) string {
	switch action {
	case ops.Stop:
		return "stopped"
	case ops.Start:
		return "started"
	}
	return action + "ed"
}

func (s *Server) handleServiceLogs(w http.ResponseWriter, r *http.Request) {
	n, _ := strconv.Atoi(r.URL.Query().Get("lines"))
	out, err := ops.ServiceLogs(stateValue("webmail"), r.PathValue("unit"), n)
	if err != nil {
		fail(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(out) == "" {
		out = "No log entries."
	}
	renderPartial(w, "services", "service_logs", map[string]any{"Logs": out})
}

// --- Mail queue -----------------------------------------------------------------------

type QueuePageData struct {
	Base
	Messages []ops.QueuedMessage
	Deferred int
	Active   int
	Hold     int
	Error    string
	Now      time.Time
}

func (s *Server) handleQueuePage(w http.ResponseWriter, r *http.Request) {
	data := QueuePageData{
		Base: s.base(w, r, "Mail queue", "queue", Crumb{Label: "Mail queue"}),
		Now:  time.Now(),
	}
	msgs, err := ops.Queue()
	if err != nil {
		data.Error = err.Error()
	}
	data.Messages = msgs
	for _, m := range msgs {
		switch m.Queue {
		case "deferred":
			data.Deferred++
		case "hold":
			data.Hold++
		default:
			data.Active++
		}
	}
	renderPage(w, "queue", data)
}

func (s *Server) handleQueueAction(w http.ResponseWriter, r *http.Request) {
	id, action := r.PathValue("id"), r.PathValue("action")
	var err error
	var msg string
	switch action {
	case "delete":
		err, msg = ops.DeleteMessage(id), "Message "+id+" deleted."
	case "retry":
		err, msg = ops.RetryMessage(id), "Retrying delivery of "+id+"."
	case "hold":
		err, msg = ops.HoldMessage(id), "Message "+id+" put on hold."
	case "release":
		err, msg = ops.ReleaseMessage(id), "Message "+id+" released."
	default:
		fail(w, r, http.StatusBadRequest, "Unknown queue action.")
		return
	}
	if err != nil {
		fail(w, r, http.StatusBadRequest, err.Error())
		return
	}
	s.record(r, "queue."+action, id, "")
	done(w, r, "/queue", msg)
}

func (s *Server) handleQueueFlush(w http.ResponseWriter, r *http.Request) {
	if err := ops.FlushQueue(); err != nil {
		fail(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.record(r, "queue.flush", "", "")
	done(w, r, "/queue", "Retrying delivery of every queued message.")
}

func (s *Server) handleQueueDeleteDeferred(w http.ResponseWriter, r *http.Request) {
	if err := ops.DeleteAllDeferred(); err != nil {
		fail(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.record(r, "queue.delete.deferred", "", "")
	done(w, r, "/queue", "Deleted every deferred message.")
}

// --- Logs ------------------------------------------------------------------------------

type LogLine struct {
	Text  string
	Level string
}

type LogsPageData struct {
	Base
	Sources []ops.LogSource
	Source  string
	Lines   int
	Query   string
	Entries []LogLine
	Error   string
}

func (s *Server) handleLogsPage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	data := LogsPageData{
		Base:    s.base(w, r, "Logs", "logs", Crumb{Label: "System"}, Crumb{Label: "Logs"}),
		Sources: ops.LogSources(),
		Source:  q.Get("source"),
		Query:   strings.TrimSpace(q.Get("q")),
	}
	data.Lines, _ = strconv.Atoi(q.Get("lines"))
	if data.Lines <= 0 {
		data.Lines = 200
	}
	if data.Source == "" && len(data.Sources) > 0 {
		data.Source = data.Sources[0].Key
	}
	lines, err := ops.TailLog(data.Source, data.Lines, data.Query)
	if err != nil {
		data.Error = err.Error()
	}
	for _, l := range lines {
		data.Entries = append(data.Entries, LogLine{Text: l, Level: ops.LineLevel(l)})
	}
	renderPage(w, "logs", data)
}
