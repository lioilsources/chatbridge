package queue

import (
	"fmt"
	"sync"
	"testing"
)

func TestSinceResetsWhenBehindWindow(t *testing.T) {
	q := New(500)
	for i := 0; i < 600; i++ {
		q.Push(Event{Type: "spawn"})
	}
	events, latest, reset := q.Since(0, 50)
	if !reset {
		t.Fatal("since=0 with 600 events in a 500 buffer must reset")
	}
	if len(events) != 0 {
		t.Errorf("reset response must be empty, got %d events", len(events))
	}
	if latest != 600 {
		t.Errorf("latest = %d, want 600", latest)
	}
}

func TestSinceReturnsNewerOnly(t *testing.T) {
	q := New(500)
	for i := 0; i < 10; i++ {
		q.Push(Event{Type: "spawn"})
	}
	events, latest, reset := q.Since(7, 50)
	if reset {
		t.Fatal("since within window must not reset")
	}
	if latest != 10 {
		t.Errorf("latest = %d, want 10", latest)
	}
	if len(events) != 3 || events[0].Seq != 8 || events[2].Seq != 10 {
		t.Fatalf("got seqs %v, want [8 9 10]", seqs(events))
	}
}

func TestSinceRespectsMax(t *testing.T) {
	q := New(500)
	for i := 0; i < 10; i++ {
		q.Push(Event{})
	}
	events, _, reset := q.Since(0, 3)
	if reset {
		t.Fatal("since=0 with 10 events in a 500 buffer is within window")
	}
	if len(events) != 3 || events[0].Seq != 1 {
		t.Fatalf("got seqs %v, want first 3 starting at 1", seqs(events))
	}
}

func TestSinceUpToDate(t *testing.T) {
	q := New(500)
	for i := 0; i < 5; i++ {
		q.Push(Event{})
	}
	events, latest, reset := q.Since(5, 50)
	if reset || len(events) != 0 || latest != 5 {
		t.Fatalf("up-to-date caller: events=%d latest=%d reset=%v, want 0/5/false",
			len(events), latest, reset)
	}
}

// TestConcurrentPushAndRead is the phase-3 acceptance under -race: concurrent
// pushers and readers, and every response must have strictly increasing,
// duplicate-free sequences.
func TestConcurrentPushAndRead(t *testing.T) {
	q := New(500)
	const pushers, readers, perPusher = 4, 4, 300

	var wg sync.WaitGroup
	errCh := make(chan error, pushers+readers)

	for p := 0; p < pushers; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perPusher; i++ {
				q.Push(Event{Type: "spawn"})
			}
		}()
	}
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				events, _, _ := q.Since(0, 500)
				var prev int64
				for _, e := range events {
					if e.Seq <= prev {
						errCh <- fmt.Errorf("seq %d not > prev %d in one response", e.Seq, prev)
						return
					}
					prev = e.Seq
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
	if got := q.Latest(); got != pushers*perPusher {
		t.Errorf("latest = %d, want %d", got, pushers*perPusher)
	}
}

func seqs(events []Event) []int64 {
	out := make([]int64, len(events))
	for i, e := range events {
		out[i] = e.Seq
	}
	return out
}
