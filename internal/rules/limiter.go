package rules

import (
	"math"
	"sync"
	"time"

	"github.com/lioilsources/chatbridge/internal/config"
)

// Clock is the limiter's only source of time, injected so tests never touch the
// real clock (plan §2 / phase 2 acceptance).
type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// SystemClock returns the real wall clock.
func SystemClock() Clock { return systemClock{} }

// Reason explains why the limiter allowed or denied an action.
type Reason int

const (
	ReasonOK Reason = iota
	ReasonUserCooldown
	ReasonGlobalBudget
	ReasonEntityCap
)

func (r Reason) String() string {
	switch r {
	case ReasonOK:
		return "ok"
	case ReasonUserCooldown:
		return "user_cooldown"
	case ReasonGlobalBudget:
		return "global_budget"
	case ReasonEntityCap:
		return "entity_cap"
	default:
		return "unknown"
	}
}

// Limiter enforces three independent limits: a per-user cooldown, a global
// token-bucket budget of actions per minute, and a cap on the number of live
// spawned entities the bridge is responsible for.
type Limiter struct {
	mu    sync.Mutex
	clock Clock

	userCooldown time.Duration
	lastByUser   map[string]time.Time

	bucketCap    float64
	refillPerSec float64
	tokens       float64
	lastRefill   time.Time

	maxEntities int
	live        []liveEntity
}

type liveEntity struct {
	expires time.Time
}

// NewLimiter builds a limiter from the configured limits.
func NewLimiter(cfg config.Limits, clock Clock) *Limiter {
	if clock == nil {
		clock = systemClock{}
	}
	capacity := float64(cfg.GlobalActionsPerMin)
	return &Limiter{
		clock:        clock,
		userCooldown: time.Duration(cfg.UserCooldownSec) * time.Second,
		lastByUser:   make(map[string]time.Time),
		bucketCap:    capacity,
		refillPerSec: capacity / 60.0,
		tokens:       capacity, // start full
		lastRefill:   clock.Now(),
		maxEntities:  cfg.MaxLiveEntities,
	}
}

// Allow decides whether an action from userHash may run now, enforcing all
// three limits. isSpawn/ttl feed the live-entity cap: only spawns with a
// positive TTL are tracked (ttl=0 is "never clean up", so it is not counted).
// On success the consumption is recorded.
func (l *Limiter) Allow(userHash string, isSpawn bool, ttl time.Duration) (bool, Reason) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.clock.Now()
	l.pruneLocked(now)

	if last, ok := l.lastByUser[userHash]; ok && now.Sub(last) < l.userCooldown {
		return false, ReasonUserCooldown
	}

	l.refillLocked(now)
	if l.tokens < 1 {
		return false, ReasonGlobalBudget
	}

	tracked := isSpawn && ttl > 0
	if tracked && len(l.live) >= l.maxEntities {
		return false, ReasonEntityCap
	}

	// All checks passed — commit.
	l.tokens--
	l.lastByUser[userHash] = now
	if tracked {
		l.live = append(l.live, liveEntity{expires: now.Add(ttl)})
	}
	return true, ReasonOK
}

// LiveCount reports how many tracked entities are still live.
func (l *Limiter) LiveCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(l.clock.Now())
	return len(l.live)
}

func (l *Limiter) refillLocked(now time.Time) {
	elapsed := now.Sub(l.lastRefill)
	if elapsed <= 0 {
		return
	}
	l.tokens = math.Min(l.bucketCap, l.tokens+elapsed.Seconds()*l.refillPerSec)
	l.lastRefill = now
}

func (l *Limiter) pruneLocked(now time.Time) {
	kept := l.live[:0]
	for _, e := range l.live {
		if e.expires.After(now) {
			kept = append(kept, e)
		}
	}
	l.live = kept
}
