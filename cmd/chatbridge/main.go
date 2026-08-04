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
