package rules

import (
	"fmt"
	"testing"
	"time"

	"github.com/lioilsources/chatbridge/internal/config"
)

type fakeClock struct{ t time.Time }

func (f *fakeClock) Now() time.Time          { return f.t }
func (f *fakeClock) advance(d time.Duration) { f.t = f.t.Add(d) }

func newClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
}

func TestUserCooldown(t *testing.T) {
	clk := newClock()
	lim := NewLimiter(config.Limits{
		UserCooldownSec: 30, GlobalActionsPerMin: 1000, MaxLiveEntities: 100,
	}, clk)

	if ok, _ := lim.Allow("userA", false, 0); !ok {
		t.Fatal("first action should be allowed")
	}
	if ok, r := lim.Allow("userA", false, 0); ok || r != ReasonUserCooldown {
		t.Fatalf("immediate repeat = (%v, %v), want denied/user_cooldown", ok, r)
	}
	clk.advance(29 * time.Second)
	if ok, r := lim.Allow("userA", false, 0); ok || r != ReasonUserCooldown {
		t.Fatalf("at 29s = (%v, %v), still within cooldown", ok, r)
	}
	// a different user is unaffected by userA's cooldown
	if ok, _ := lim.Allow("userB", false, 0); !ok {
		t.Fatal("other user should not be on cooldown")
	}
	clk.advance(1 * time.Second) // userA now 30s later
	if ok, r := lim.Allow("userA", false, 0); !ok {
		t.Fatalf("at 30s = denied (%v), cooldown should have elapsed", r)
	}
}

func TestGlobalBudgetAndRefill(t *testing.T) {
	clk := newClock()
	lim := NewLimiter(config.Limits{
		UserCooldownSec: 0, GlobalActionsPerMin: 3, MaxLiveEntities: 100,
	}, clk)

	for i := 0; i < 3; i++ {
		if ok, r := lim.Allow(fmt.Sprintf("u%d", i), false, 0); !ok {
			t.Fatalf("take %d = denied (%v), budget of 3 not exhausted yet", i, r)
		}
	}
	if ok, r := lim.Allow("u4", false, 0); ok || r != ReasonGlobalBudget {
		t.Fatalf("4th take = (%v, %v), want denied/global_budget", ok, r)
	}
	// refill rate = 3/min = one token per 20s
	clk.advance(20 * time.Second)
	if ok, r := lim.Allow("u5", false, 0); !ok {
		t.Fatalf("after 20s refill = denied (%v), want one token back", r)
	}
}

func TestEntityCapAndExpiry(t *testing.T) {
	clk := newClock()
	lim := NewLimiter(config.Limits{
		UserCooldownSec: 0, GlobalActionsPerMin: 1000, MaxLiveEntities: 2,
	}, clk)
	ttl := 60 * time.Second

	if ok, _ := lim.Allow("a", true, ttl); !ok {
		t.Fatal("spawn 1 should be allowed")
	}
	if ok, _ := lim.Allow("b", true, ttl); !ok {
		t.Fatal("spawn 2 should be allowed")
	}
	if ok, r := lim.Allow("c", true, ttl); ok || r != ReasonEntityCap {
		t.Fatalf("spawn 3 = (%v, %v), want denied/entity_cap", ok, r)
	}
	if n := lim.LiveCount(); n != 2 {
		t.Fatalf("LiveCount = %d, want 2", n)
	}
	// a persistent spawn (ttl=0) is not tracked, so the cap does not apply
	if ok, _ := lim.Allow("d", true, 0); !ok {
		t.Fatal("ttl=0 spawn should not be blocked by the entity cap")
	}
	// once the tracked entities expire, new spawns are allowed again
	clk.advance(61 * time.Second)
	if n := lim.LiveCount(); n != 0 {
		t.Fatalf("LiveCount after expiry = %d, want 0", n)
	}
	if ok, _ := lim.Allow("e", true, ttl); !ok {
		t.Fatal("spawn should be allowed after entities expired")
	}
}
