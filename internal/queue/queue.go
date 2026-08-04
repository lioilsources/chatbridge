// Package queue is the bridge's in-memory event log: a fixed-size ring buffer
// the Luanti mod polls via GET /events?since=N. No database — a restart is a
// clean slate (plan non-goals). The Event shape is fixed (plan §2).
package queue

import "sync"

// Event is one thing the game should do. It carries no nickname — names go to
// the overlay on a separate channel, never into the game world (plan §2/§6).
type Event struct {
	Seq    int64          `json:"seq"`
	TS     int64          `json:"ts"`
	Type   string         `json:"type"`
	Action string         `json:"action"`
	Params map[string]any `json:"params,omitempty"`
	TTL    int            `json:"ttl"`
}

// Queue is a thread-safe ring buffer with a monotonic sequence number that
// never resets while the process runs.
type Queue struct {
	mu   sync.Mutex
	buf  []Event
	cap  int
	head int   // index of the oldest retained event
	n    int   // number of retained events (0..cap)
	seq  int64 // highest sequence assigned so far
}

// New creates a ring buffer retaining the most recent size events.
func New(size int) *Queue {
	if size < 1 {
		size = 1
	}
	return &Queue{buf: make([]Event, size), cap: size}
}

// Push assigns the next sequence number to e, stores it (evicting the oldest
// event when full) and returns the assigned sequence.
func (q *Queue) Push(e Event) int64 {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.seq++
	e.Seq = q.seq
	if q.n < q.cap {
		q.buf[(q.head+q.n)%q.cap] = e
		q.n++
	} else {
		q.buf[q.head] = e // overwrite the oldest
		q.head = (q.head + 1) % q.cap
	}
	return q.seq
}

// Since returns events with Seq > since (oldest first, at most max), the latest
// sequence, and reset. reset is true when the caller has fallen outside the
// retained window (since predates it) or is somehow ahead of us (we restarted):
// in both cases it must resync to latest rather than replay history.
func (q *Queue) Since(since int64, max int) (events []Event, latest int64, reset bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	latest = q.seq
	if q.n == 0 {
		return []Event{}, latest, since > latest
	}

	oldest := q.buf[q.head].Seq
	// since+1 is the caller's next expected seq. If it predates the oldest
	// retained event, the gap can't be served. If since is beyond latest, the
	// bridge restarted and rewound below the caller. Either way: resync.
	if since < oldest-1 || since > latest {
		return []Event{}, latest, true
	}

	if max <= 0 || max > q.cap {
		max = q.cap
	}
	events = make([]Event, 0, max)
	for i := 0; i < q.n; i++ {
		e := q.buf[(q.head+i)%q.cap]
		if e.Seq > since {
			events = append(events, e)
			if len(events) >= max {
				break
			}
		}
	}
	return events, latest, false
}

// Latest returns the highest sequence assigned so far (0 if none).
func (q *Queue) Latest() int64 {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.seq
}
