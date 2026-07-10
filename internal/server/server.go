// Package server builds the process's http.Handler: ops endpoints
// (/healthz, /metrics) matched before host routing and bypassing
// middleware, then host-normalizing routing to the apex mux or a
// not-found response. See 01-architecture.md "HTTP routing".
package server

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"nyx-drop/internal/clock"
	"nyx-drop/internal/config"
	"nyx-drop/internal/metrics"
)

// HealthFunc reports process readiness. S1 always returns nil; CARD-002
// supplies the real DB-ping + data-dir-writability check.
type HealthFunc func(context.Context) error

// Deps are server.New's dependencies, as a struct (not positional args) so
// later cards add wiring (store, locks, auth) without breaking call sites.
type Deps struct {
	Config   *config.Config
	Clock    clock.Clock
	Logger   *slog.Logger
	Metrics  *metrics.Metrics
	Registry *prometheus.Registry // for the /metrics handler
	Health   HealthFunc
}

// srv holds the wiring New assembles: the rendered not-found page (cached
// at startup, not per-request), the apex mux (empty in S1; CARD-007/008/009
// add patterns), the /metrics handler, and the middleware-wrapped router.
type srv struct {
	deps          Deps
	notFoundBytes []byte
	apexMux       *http.ServeMux
	metricsH      http.Handler
	routed        http.Handler
}

// New renders the 404 page once and wires routing + middleware into a
// single http.Handler.
func New(d Deps) (http.Handler, error) {
	notFoundBytes, err := renderNotFound(d.Config)
	if err != nil {
		return nil, err
	}

	s := &srv{
		deps:          d,
		notFoundBytes: notFoundBytes,
		apexMux:       http.NewServeMux(),
		metricsH:      promhttp.HandlerFor(d.Registry, promhttp.HandlerOpts{}),
	}
	s.routed = requestLog(d.Logger)(instrument(d.Metrics, d.Config)(http.HandlerFunc(s.rootHost)))

	return http.HandlerFunc(s.top), nil
}

// top matches /healthz and /metrics on any Host before host routing and
// middleware; everything else goes through requestLog -> instrument ->
// rootHost.
func (s *srv) top(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/healthz":
		s.health(w, r)
		return
	case "/metrics":
		s.metricsH.ServeHTTP(w, r)
		return
	}
	s.routed.ServeHTTP(w, r)
}

// rootHost classifies the normalized Host: the apex goes to apexMux (an
// empty *http.ServeMux in S1, so it 404s with Go's plain not-found body
// until CARD-007/008/009 add patterns); a valid site-host label and any
// other host both render the branded not-found page (CARD-003 replaces the
// site-host branch with real serving).
func (s *srv) rootHost(w http.ResponseWriter, r *http.Request) {
	cfg := s.deps.Config
	host := normalizeHost(r.Host)

	if host == cfg.BaseDomain {
		s.apexMux.ServeHTTP(w, r)
		return
	}
	if _, ok := siteLabel(host, cfg.BaseDomain); ok {
		s.serveNotFound(w, r) // valid site host, no store yet
		return
	}
	s.serveNotFound(w, r) // unknown or multi-label host
}

// health answers 200 "ok" when Health succeeds, else 503.
func (s *srv) health(w http.ResponseWriter, r *http.Request) {
	if err := s.deps.Health(r.Context()); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// serveNotFound renders the cached branded 404 page.
func (s *srv) serveNotFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusNotFound)
	w.Write(s.notFoundBytes)
}
