package webui

import (
	"net/http"

	"github.com/itsrifathridoy/patrabahok/cli/internal/sysinfo"
)

type QueuePageData struct {
	Base
	Queue string
}

func (s *Server) queueData(r *http.Request) QueuePageData {
	out, _ := sysinfo.QueueList()
	return QueuePageData{
		Base:  Base{Title: "Mail queue", Active: "queue", Username: userFromContext(r).Username},
		Queue: out,
	}
}

func (s *Server) handleQueuePage(w http.ResponseWriter, r *http.Request) {
	renderPage(w, "queue", s.queueData(r))
}

func (s *Server) handleQueuePartial(w http.ResponseWriter, r *http.Request) {
	renderPartial(w, "queue", "queue_body", s.queueData(r))
}

func (s *Server) handleQueueFlush(w http.ResponseWriter, r *http.Request) {
	_, _ = sysinfo.QueueFlush()
	renderPartial(w, "queue", "queue_body", s.queueData(r))
}
