package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lioilsources/chatbridge/internal/queue"
)

const token = "s3cret"

func newTestServer() *Server {
	q := queue.New(10)
	q.Push(queue.Event{Type: "spawn", Action: "chicken"})
	return New(Options{
		Queue:      q,
		Gate:       NewGate(),
		AdminToken: token,
		LiveCount:  func() int { return 7 },
	})
}

func do(t *testing.T, s *Server, method, path, tok string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if tok != "" {
		req.Header.Set("X-Bridge-Token", tok)
	}
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	return rec
}

func TestEventsStaysPublic(t *testing.T) {
	// The mod cannot authenticate, so /events must never require a token.
	if got := do(t, newTestServer(), "GET", "/events?since=0", "").Code; got != http.StatusOK {
		t.Fatalf("GET /events = %d, want 200", got)
	}
}

func TestAdminRequiresToken(t *testing.T) {
	s := newTestServer()
	for _, tc := range []struct{ name, tok string }{
		{"missing", ""},
		{"wrong", "nope"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := do(t, s, "POST", "/admin/off", tc.tok).Code; got != http.StatusForbidden {
				t.Fatalf("token %q = %d, want 403", tc.tok, got)
			}
			if !s.gate.Open() {
				t.Fatal("unauthorized request must not flip the gate")
			}
		})
	}
}

func TestAdminEmptyTokenDeniesEverything(t *testing.T) {
	s := New(Options{Queue: queue.New(4)}) // no AdminToken configured
	if got := do(t, s, "GET", "/admin/status", "").Code; got != http.StatusForbidden {
		t.Fatalf("unset token = %d, want 403", got)
	}
}

func TestAdminOffOnRoundTrip(t *testing.T) {
	s := newTestServer()

	rec := do(t, s, "POST", "/admin/off", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("off = %d, want 200", rec.Code)
	}
	var st statusResponse
	if err := json.NewDecoder(rec.Body).Decode(&st); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if st.Enabled {
		t.Fatal("status after /admin/off must report enabled=false")
	}
	if s.gate.Open() {
		t.Fatal("gate still open after /admin/off")
	}

	if do(t, s, "POST", "/admin/on", token).Code != http.StatusOK {
		t.Fatal("on failed")
	}
	if !s.gate.Open() {
		t.Fatal("gate not reopened")
	}
}

func TestAdminStatusReportsCounts(t *testing.T) {
	rec := do(t, newTestServer(), "GET", "/admin/status", token)
	var st statusResponse
	if err := json.NewDecoder(rec.Body).Decode(&st); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if st.Events != 1 || st.Live != 7 || st.Mode != "individual" {
		t.Fatalf("status = %+v, want events=1 live=7 mode=individual", st)
	}
}
