// Package api serves the mod-facing HTTP surface. The mod POLLs GET /events —
// Luanti Lua has no sockets. The contract is fixed (plan §2).
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/lioilsources/chatbridge/internal/queue"
)

const defaultMax = 50

// Server holds the dependencies for the HTTP handlers.
type Server struct {
	queue *queue.Queue
	mode  func() string // "individual" | "vote"; phase 6 makes it dynamic
}

// New builds the API server. mode may be nil (defaults to "individual").
func New(q *queue.Queue, mode func() string) *Server {
	if mode == nil {
		mode = func() string { return "individual" }
	}
	return &Server{queue: q, mode: mode}
}

// Routes returns the HTTP mux for the service.
func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /events", s.events)
	return mux
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}

type eventsResponse struct {
	Events []queue.Event `json:"events"`
	Latest int64         `json:"latest"`
	Mode   string        `json:"mode,omitempty"`
	Reset  bool          `json:"reset,omitempty"`
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	since := parseInt64(r.URL.Query().Get("since"), 0)
	max := int(parseInt64(r.URL.Query().Get("max"), defaultMax))
	if max <= 0 {
		max = defaultMax
	}

	events, latest, reset := s.queue.Since(since, max)
	resp := eventsResponse{Events: events, Latest: latest}
	if reset {
		resp.Reset = true // omit mode on reset (plan §2)
	} else {
		resp.Mode = s.mode()
	}
	writeJSON(w, resp)
}

func parseInt64(s string, def int64) int64 {
	if s == "" {
		return def
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return def
	}
	return n
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("encode response failed", "err", err)
	}
}
