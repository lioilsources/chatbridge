// Package api serves the mod-facing HTTP surface. The mod POLLs GET /events —
// Luanti Lua has no sockets. The contract is fixed (plan §2).
package api

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/lioilsources/chatbridge/internal/queue"
)

const defaultMax = 50

// Options collects the API server dependencies. Only Queue is required.
type Options struct {
	Queue      *queue.Queue
	Mode       func() string // "individual" | "vote"; phase 6 makes it dynamic
	Gate       *Gate         // kill switch toggled by /admin/on|off
	AdminToken string        // shared secret for X-Bridge-Token
	LiveCount  func() int    // tracked live entities, for /admin/status
}

// Server holds the dependencies for the HTTP handlers.
type Server struct {
	queue      *queue.Queue
	mode       func() string
	gate       *Gate
	adminToken string
	liveCount  func() int
}

// New builds the API server, filling in inert defaults for optional fields.
func New(opts Options) *Server {
	if opts.Mode == nil {
		opts.Mode = func() string { return "individual" }
	}
	if opts.Gate == nil {
		opts.Gate = NewGate()
	}
	if opts.LiveCount == nil {
		opts.LiveCount = func() int { return 0 }
	}
	return &Server{
		queue:      opts.Queue,
		mode:       opts.Mode,
		gate:       opts.Gate,
		adminToken: opts.AdminToken,
		liveCount:  opts.LiveCount,
	}
}

// Routes returns the HTTP mux for the service. /events stays public — the mod
// cannot authenticate; /admin/* is behind the shared token (plan §2).
func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /events", s.events)
	mux.HandleFunc("POST /admin/off", s.admin(func() { s.gate.Set(false) }))
	mux.HandleFunc("POST /admin/on", s.admin(func() { s.gate.Set(true) }))
	mux.HandleFunc("GET /admin/status", s.admin(nil))
	return mux
}

type statusResponse struct {
	Enabled bool   `json:"enabled"`
	Events  int64  `json:"events"`
	Mode    string `json:"mode"`
	Live    int    `json:"live_entities"`
}

// admin wraps a handler with token auth; act may be nil for read-only status.
// Every admin route answers with the current status so callers see the effect.
func (s *Server) admin(act func()) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if act != nil {
			act()
			slog.Info("admin toggled ingest", "enabled", s.gate.Open())
		}
		writeJSON(w, statusResponse{
			Enabled: s.gate.Open(),
			Events:  s.queue.Latest(),
			Mode:    s.mode(),
			Live:    s.liveCount(),
		})
	}
}

// authorized compares the token in constant time. An unset token denies
// everything rather than opening the door.
func (s *Server) authorized(r *http.Request) bool {
	if s.adminToken == "" {
		return false
	}
	got := r.Header.Get("X-Bridge-Token")
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.adminToken)) == 1
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
