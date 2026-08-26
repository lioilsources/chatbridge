// Command chatbridge is the Twitch-chat → Luanti bridge service.
//
// Pipeline: anonymous Twitch ingest → parse against the action catalog → rate
// limits → in-memory event queue, which the Luanti mod polls via GET /events.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/lioilsources/chatbridge/internal/api"
	"github.com/lioilsources/chatbridge/internal/config"
	"github.com/lioilsources/chatbridge/internal/queue"
	"github.com/lioilsources/chatbridge/internal/rules"
	"github.com/lioilsources/chatbridge/internal/twitch"
)

func main() {
	cfgPath := flag.String("config", "config.example.yaml", "path to config file")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		slog.Error("config load failed", "path", *cfgPath, "err", err)
		os.Exit(1)
	}
	slog.Info("config loaded",
		"channel", cfg.Twitch.Channel,
		"addr", cfg.Server.Addr,
		"actions", len(cfg.Actions))

	// The event log the mod polls, and the HTTP surface over it.
	q := queue.New(cfg.Limits.QueueSize)
	gate := api.NewGate()
	limiter := rules.NewLimiter(cfg.Limits, rules.SystemClock())
	srv := &http.Server{
		Addr: cfg.Server.Addr,
		Handler: api.New(api.Options{
			Queue:      q,
			Gate:       gate,
			AdminToken: cfg.Server.AdminToken,
			LiveCount:  limiter.LiveCount,
		}).Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	// SIGINT/SIGTERM cancel the root context and trigger graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Chat → intent → limits → queue. Unknown commands are dropped silently;
	// the bridge never writes back to chat.
	catalog := rules.NewCatalog(cfg.Actions)

	// Panic brake: the first action refused because the world is full emits one
	// system/throttled event so the mod (and later the overlay) can say why
	// nothing is happening. Re-armed as soon as an action gets through, so a
	// sustained flood cannot spam the queue with warnings.
	// atomic: go-twitch-irc dispatches serially today, but this flag is not
	// worth a subtle race if that ever changes.
	var throttled atomic.Bool

	tw := twitch.New(cfg.Twitch.Channel, func(m twitch.Message) {
		if !gate.Open() {
			return // killed by /admin/off
		}
		action, ok := catalog.Parse(m.Text)
		if !ok {
			return // not a command
		}
		isSpawn := action.Type == "spawn"
		ttl := time.Duration(action.TTL) * time.Second
		if ok, reason := limiter.Allow(rules.HashUser(m.User), isSpawn, ttl); !ok {
			slog.Debug("action dropped", "action", action.Name, "reason", reason.String())
			if reason == rules.ReasonEntityCap && throttled.CompareAndSwap(false, true) {
				q.Push(queue.Event{
					TS:     time.Now().Unix(),
					Type:   "system",
					Action: "throttled",
					Params: map[string]any{"live": limiter.LiveCount()},
				})
				slog.Warn("entity cap reached, spawns paused", "live", limiter.LiveCount())
			}
			return
		}
		throttled.Store(false)
		seq := q.Push(queue.Event{
			TS:     time.Now().Unix(),
			Type:   action.Type,
			Action: action.Name,
			Params: action.Params,
			TTL:    action.TTL,
		})
		slog.Info("action enqueued", "seq", seq, "action", action.Name, "type", action.Type)
	}, slog.Default())
	go func() {
		if err := tw.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("twitch ingest stopped", "err", err)
		}
	}()

	go func() {
		slog.Info("http server listening", "addr", cfg.Server.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server error", "err", err)
			stop() // unblock main so the process exits instead of hanging
		}
	}()

	<-ctx.Done()
	slog.Info("shutdown signal received, draining")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", "err", err)
		os.Exit(1)
	}
	slog.Info("stopped cleanly")
}
