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

func main() {
	if err := run(os.Getenv); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run builds and serves the process. It is separated from main so tests
// can drive it with a fake getenv rather than mutating the real process
// environment; config.Load and server.New already cover the fail-fast and
// routing behavior this wires together (tasks 4/10), so run's own
// responsibility is startup wiring and graceful shutdown.
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

	httpServer := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

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
