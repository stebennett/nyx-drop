// Package server builds the process's http.Handler: ops endpoints
// (/healthz, /metrics) matched before host routing and bypassing
// middleware, then host-normalizing routing to the apex mux or an
// uploaded site. See 01-architecture.md "HTTP routing".
package server

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"nyx-drop/internal/auth"
	"nyx-drop/internal/clock"
	"nyx-drop/internal/config"
	"nyx-drop/internal/metrics"
	"nyx-drop/internal/store"
)

// HealthFunc reports process readiness. In production it checks the DB and
// data directory.
type HealthFunc func(context.Context) error

// Deps are server.New's dependencies, as a struct (not positional args) so
// later cards add wiring (store, locks, auth) without breaking call sites.
type Deps struct {
	Config          *config.Config
	Clock           clock.Clock
	Logger          *slog.Logger
	Metrics         *metrics.Metrics
	Registry        *prometheus.Registry // for the /metrics handler
	Health          HealthFunc
	Store           *store.DB            // nil only for routing/readiness fixtures
	GitHubEndpoints auth.GitHubEndpoints // zero value uses GitHub; tests can inject a stub
}

// srv holds the wiring New assembles, including the cached not-found page.
type srv struct {
	deps          Deps
	notFoundBytes []byte
	apexMux       *http.ServeMux
	metricsH      http.Handler
	routed        http.Handler
	sites         *siteRoutes
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
	if d.Store != nil {
		if err := s.registerSiteRoutes(); err != nil {
			return nil, err
		}
	}
	if err := registerAdminRoutes(s.apexMux, d); err != nil {
		return nil, err
	}
	s.routed = requestLog(d.Logger)(instrument(d.Metrics, d.Config)(http.HandlerFunc(s.rootHost)))

	return s, nil
}

// ServeHTTP matches /healthz and /metrics on any Host before host routing and
// middleware; everything else goes through requestLog -> instrument ->
// rootHost.
func (s *srv) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

// rootHost routes the normalized Host to the apex, a site, or a branded 404.
func (s *srv) rootHost(w http.ResponseWriter, r *http.Request) {
	cfg := s.deps.Config
	host := normalizeHost(r.Host)

	if host == cfg.BaseDomain {
		s.apexMux.ServeHTTP(w, r)
		return
	}
	if id, ok := siteLabel(host, cfg.BaseDomain); ok && s.sites != nil {
		s.sites.serve(w, r, id)
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
	if r.Method != http.MethodHead {
		w.Write(s.notFoundBytes)
	}
}
