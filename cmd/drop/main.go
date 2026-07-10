// Command drop is the nyx-drop process entrypoint: parses configuration,
// wires the server, and serves until SIGINT/SIGTERM triggers a graceful
// shutdown.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"nyx-drop/internal/clock"
	"nyx-drop/internal/config"
	"nyx-drop/internal/metrics"
	"nyx-drop/internal/server"
)

// idleTimeout bounds how long a keep-alive connection may sit idle
// between requests before the server closes it. Without this, idle
// connections are never reaped (IdleTimeout falls back to ReadTimeout,
// not ReadHeaderTimeout, and both were previously zero/unlimited) — a
// connection-hold resource-exhaustion vector. WriteTimeout is
// deliberately left unset: CARD-003/004 add real site/upload responses
// bounded by MAX_SITE_SIZE/MAX_UPLOAD_SIZE, and a fixed WriteTimeout set
// now could truncate those later without knowing their size/bandwidth
// envelope.
const idleTimeout = 120 * time.Second

// newHTTPServer builds the http.Server with its timeout policy, split
// out so the policy is unit-testable without binding a real listener.
func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       idleTimeout,
	}
}

func main() {
	if err := run(os.Getenv); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run builds and serves the process. It is separated from main so tests
// can drive it with a fake getenv rather than mutating the real process
// environment; config.Load and server.New already cover the fail-fast and
// routing behavior this wires together, so run's own responsibility is
// startup wiring and graceful shutdown.
func run(getenv func(string) string) error {
	cfg, err := config.Load(getenv)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	clk := clock.Real{}
	reg := prometheus.NewRegistry()
	m := metrics.New(reg)

	// S1 readiness is a stub that always succeeds; CARD-002 replaces this
	// with a real DB-ping + data-dir-writability check.
	health := func(context.Context) error { return nil }

	handler, err := server.New(server.Deps{
		Config:   cfg,
		Clock:    clk,
		Logger:   logger,
		Metrics:  m,
		Registry: reg,
		Health:   health,
	})
	if err != nil {
		return fmt.Errorf("server: %w", err)
	}

	httpServer := newHTTPServer(cfg.Addr(), handler)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.Addr())
		serveErr <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("listen: %w", err)
		}
		return nil
	case <-ctx.Done():
		stop()
		logger.Info("shutting down")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return nil
	}
}
