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
type Metrics struct {
	HTTPDuration *prometheus.HistogramVec
	SitesCreated prometheus.Counter
	UploadBytes  prometheus.Counter
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
		SitesCreated: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "sites_created_total",
			Help: "Total number of sites successfully published.",
		}),
		UploadBytes: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "upload_bytes_total",
			Help: "Total uploaded ZIP bytes for successfully published sites.",
		}),
	}
	reg.MustRegister(m.HTTPDuration, m.SitesCreated, m.UploadBytes)

	return m
}
