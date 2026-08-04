// Command chatbridge is the Twitch-chat → Luanti bridge service.
//
// Phase 0: config, /healthz, structured logging, graceful shutdown. Twitch
// ingest, the event queue and the rest arrive in later phases.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lioilsources/chatbridge/internal/config"
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

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// SIGINT/SIGTERM cancel the root context and trigger graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Chat → intent → limits. Unknown commands are dropped silently; the
	// bridge never writes back to chat. Phase 3 enqueues accepted actions.
	catalog := rules.NewCatalog(cfg.Actions)
	limiter := rules.NewLimiter(cfg.Limits, rules.SystemClock())

	tw := twitch.New(cfg.Twitch.Channel, func(m twitch.Message) {
		action, ok := catalog.Parse(m.Text)
		if !ok {
			return // not a command
		}
		isSpawn := action.Type == "spawn"
		ttl := time.Duration(action.TTL) * time.Second
		if ok, reason := limiter.Allow(rules.HashUser(m.User), isSpawn, ttl); !ok {
			slog.Debug("action dropped", "action", action.Name, "reason", reason.String())
			return
		}
		slog.Info("action accepted", "action", action.Name, "type", action.Type)
		// phase 3: turn this into an Event and push it on the queue
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
