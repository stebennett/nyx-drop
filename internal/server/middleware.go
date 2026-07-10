package server

import (
	"log/slog"
	"net/http"
	"time"

	"nyx-drop/internal/config"
	"nyx-drop/internal/metrics"
)

// responseRecorder wraps an http.ResponseWriter to capture the status code
// and byte count written, for requestLog and instrument. It defaults to
// 200 if the handler never calls WriteHeader explicitly (matching
// net/http's own behavior on first Write).
type responseRecorder struct {
	http.ResponseWriter
	status      int
	bytes       int
	wroteHeader bool
}

func newResponseRecorder(w http.ResponseWriter) *responseRecorder {
	return &responseRecorder{ResponseWriter: w, status: http.StatusOK}
}

func (rr *responseRecorder) WriteHeader(status int) {
	if !rr.wroteHeader {
		rr.status = status
		rr.wroteHeader = true
	}
	rr.ResponseWriter.WriteHeader(status)
}

func (rr *responseRecorder) Write(b []byte) (int, error) {
	if !rr.wroteHeader {
		rr.wroteHeader = true
	}
	n, err := rr.ResponseWriter.Write(b)
	rr.bytes += n
	return n, err
}

// Unwrap exposes the underlying ResponseWriter to http.NewResponseController,
// so future handlers can reach Flusher/Hijacker through the recorder.
func (rr *responseRecorder) Unwrap() http.ResponseWriter {
	return rr.ResponseWriter
}

// requestLog logs one JSON line per request via log, after the handler
// completes: msg="request" with host, path, method, status, dur_ms, bytes.
// Latency is measured with the monotonic wall clock (time.Now/time.Since),
// not the injected clock.Clock — latency is not business time, and a
// frozen fake clock would always report dur_ms=0.
func requestLog(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rr := newResponseRecorder(w)
			next.ServeHTTP(rr, r)
			dur := time.Since(start)
			log.Info("request",
				"host", r.Host,
				"path", r.URL.Path,
				"method", r.Method,
				"status", rr.status,
				"dur_ms", dur.Milliseconds(),
				"bytes", rr.bytes,
			)
		})
	}
}

// instrument observes the request's duration in seconds against
// m.HTTPDuration, labeled by routeClass(cfg, r).
func instrument(m *metrics.Metrics, cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			next.ServeHTTP(w, r)
			dur := time.Since(start)
			m.HTTPDuration.WithLabelValues(routeClass(cfg, r)).Observe(dur.Seconds())
		})
	}
}
