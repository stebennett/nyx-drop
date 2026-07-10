// Package metrics wires the Prometheus registry used across the process:
// the standard Go/process collectors plus the app's own HTTP request
// histogram. There is no global/promauto registry — callers construct a
// prometheus.NewRegistry() and pass it to New, so tests never share state.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Metrics holds the collectors this process exposes on /metrics.
// Lifecycle counters/gauges (sites_active, sites_created_total, ...) are
// added by the cards that emit them.
type Metrics struct {
	HTTPDuration *prometheus.HistogramVec
}

// New registers the Go runtime collector, the process collector, and the
// HTTP request duration histogram on reg, and returns the handle used to
// observe requests.
func New(reg prometheus.Registerer) *Metrics {
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	m := &Metrics{
		HTTPDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "http_request_duration_seconds",
			Help: "HTTP request duration in seconds, by route class.",
		}, []string{"class"}),
	}
	reg.MustRegister(m.HTTPDuration)

	return m
}
