package twitch

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

func quietClient() *Client {
	return New("chan", func(Message) {}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestRunRetriesWithBackoffUntilCancel(t *testing.T) {
	c := quietClient()
	c.baseDelay = 3 * time.Millisecond
	c.maxDelay = 12 * time.Millisecond

	var mu sync.Mutex
	calls := 0
	c.connectOnce = func(context.Context) error {
		mu.Lock()
		calls++
		mu.Unlock()
		return errors.New("dial failed") // fail instantly → loop should retry
	}

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if err := c.Run(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run returned %v, want context.DeadlineExceeded", err)
	}
	mu.Lock()
	n := calls
	mu.Unlock()
	if n < 3 {
		t.Errorf("connectOnce called %d times, want several retries", n)
	}
}

func TestRunStopsPromptlyOnCancel(t *testing.T) {
	c := quietClient()
	// a healthy connection that blocks until ctx is cancelled
	c.connectOnce = func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run returned %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return promptly after cancel")
	}
}

func TestBackoffGrowsAndCaps(t *testing.T) {
	base := time.Second
	max := 60 * time.Second
	const neutral = 0.5 // no jitter offset

	// exponential from base, capped at max
	want := map[int]time.Duration{
		0: 1 * time.Second,
		1: 2 * time.Second,
		2: 4 * time.Second,
		5: 32 * time.Second,
		6: 60 * time.Second, // 64s → capped
		20: 60 * time.Second,
	}
	for attempt, exp := range want {
		if got := backoff(attempt, base, max, neutral); got != exp {
			t.Errorf("backoff(%d) = %s, want %s", attempt, got, exp)
		}
	}
}

func TestBackoffJitterWithinBounds(t *testing.T) {
	base := time.Second
	max := 60 * time.Second
	// attempt 3 → 8s base; ±25% → [6s, 10s]
	for _, j := range []float64{0, 0.25, 0.5, 0.75, 0.999} {
		got := backoff(3, base, max, j)
		if got < 6*time.Second || got > 10*time.Second {
			t.Errorf("backoff(3, jitter=%v) = %s, want within [6s,10s]", j, got)
		}
	}
}

func TestBackoffClampsToBaseFloor(t *testing.T) {
	base := time.Second
	max := 60 * time.Second
	// even with minimum jitter the delay never drops below base
	if got := backoff(0, base, max, 0); got < base {
		t.Errorf("backoff(0, jitter=0) = %s, want >= base %s", got, base)
	}
	// negative attempt is treated as 0, not a panic/huge shift
	if got := backoff(-5, base, max, 0.5); got != base {
		t.Errorf("backoff(-5) = %s, want %s", got, base)
	}
}
