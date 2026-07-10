package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"nyx-drop/internal/clock"
	"nyx-drop/internal/config"
	"nyx-drop/internal/metrics"
)

const testBaseDomain = "test.local"

// newTestServer builds a server.New handler wired with test doubles: a
// fake clock, a fresh Prometheus registry, a discard-writer logger, and a
// Health stub that always succeeds. logWriter defaults to io.Discard when
// nil so tests that don't care about log output don't need to drain a
// buffer.
func newTestServer(t *testing.T, logWriter io.Writer) (http.Handler, *clock.Fake) {
	t.Helper()

	if logWriter == nil {
		logWriter = io.Discard
	}

	cfg := &config.Config{BaseDomain: testBaseDomain, Scheme: "http"}
	fc := clock.NewFake(time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC))
	reg := prometheus.NewRegistry()
	m := metrics.New(reg)
	log := slog.New(slog.NewJSONHandler(logWriter, nil))

	h, err := New(Deps{
		Config:   cfg,
		Clock:    fc,
		Logger:   log,
		Metrics:  m,
		Registry: reg,
		Health:   func(context.Context) error { return nil },
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	return h, fc
}

func TestHealthz_AnyHost_200(t *testing.T) {
	h, _ := newTestServer(t, nil)

	hosts := []string{"sites.nyxhub.net", "anything.example", "10.0.0.5:8080", ""}
	for _, host := range hosts {
		t.Run("host="+host, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			req.Host = host
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("status = %d, want 200", rec.Code)
			}
			if rec.Body.String() != "ok" {
				t.Errorf("body = %q, want %q", rec.Body.String(), "ok")
			}
		})
	}
}

func TestMetrics_AnyHost_Exposition(t *testing.T) {
	h, _ := newTestServer(t, nil)

	// Generate one instrumented observation first so
	// http_request_duration_seconds has a sample to expose (a HistogramVec
	// with no observations yet emits no series).
	warm := httptest.NewRequest(http.MethodGet, "/", nil)
	warm.Host = testBaseDomain
	h.ServeHTTP(httptest.NewRecorder(), warm)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Host = "anything.example"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"go_goroutines", "process_", "http_request_duration_seconds"} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics body missing %q; body:\n%s", want, body)
		}
	}
}

func TestOpsEndpoints_NotLogged(t *testing.T) {
	var buf strings.Builder
	h, _ := newTestServer(t, &buf)

	for _, path := range []string{"/healthz", "/metrics"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = testBaseDomain
		h.ServeHTTP(httptest.NewRecorder(), req)
	}

	if buf.Len() != 0 {
		t.Errorf("expected no log output for ops endpoints, got:\n%s", buf.String())
	}
}

func TestUnknownHost_Branded404(t *testing.T) {
	h, _ := newTestServer(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "unrelated.example.com"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assertBranded404(t, rec)
}

func TestMultiLabelHost_404(t *testing.T) {
	h, _ := newTestServer(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "a.b." + testBaseDomain
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assertBranded404(t, rec)
}

func TestSiteHost_404UntilStore(t *testing.T) {
	h, _ := newTestServer(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "trusty-tahr." + testBaseDomain
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assertBranded404(t, rec)
}

func TestApex_404Placeholder(t *testing.T) {
	h, _ := newTestServer(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = testBaseDomain
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "faded into the night") {
		t.Errorf("apex 404 should be Go's plain ServeMux placeholder, not the branded page; body:\n%s", rec.Body.String())
	}
}

func assertBranded404(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html prefix", ct)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want %q", got, "nosniff")
	}
	if !strings.Contains(rec.Body.String(), "faded into the night") {
		t.Errorf("body missing branded copy; body:\n%s", rec.Body.String())
	}
}
