package api

import "sync/atomic"

// Gate is the kill switch: when closed the ingest pipeline drops every action
// before it reaches the queue. Flipped by POST /admin/off and /admin/on, and
// read on the hot path of every chat message, so it is a plain atomic.
type Gate struct {
	open atomic.Bool
}

// NewGate returns an open gate — the service accepts actions by default.
func NewGate() *Gate {
	g := &Gate{}
	g.open.Store(true)
	return g
}

// Open reports whether actions may flow.
func (g *Gate) Open() bool { return g.open.Load() }

// Set opens or closes the gate.
func (g *Gate) Set(open bool) { g.open.Store(open) }
