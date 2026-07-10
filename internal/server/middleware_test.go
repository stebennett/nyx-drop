package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"nyx-drop/internal/config"
	"nyx-drop/internal/metrics"
)

func TestResponseRecorder_CapturesStatusAndBytes(t *testing.T) {
	rec := httptest.NewRecorder()
	rr := newResponseRecorder(rec)

	rr.WriteHeader(http.StatusTeapot)
	n, err := rr.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write() error: %v", err)
	}
	if n != 5 {
		t.Errorf("Write() = %d, want 5", n)
	}

	if rr.status != http.StatusTeapot {
		t.Errorf("status = %d, want %d", rr.status, http.StatusTeapot)
	}
	if rr.bytes != 5 {
		t.Errorf("bytes = %d, want 5", rr.bytes)
	}
}

func TestResponseRecorder_DefaultStatusIsOK(t *testing.T) {
	rec := httptest.NewRecorder()
	rr := newResponseRecorder(rec)

	if _, err := rr.Write([]byte("no explicit WriteHeader")); err != nil {
		t.Fatalf("Write() error: %v", err)
	}

	if rr.status != http.StatusOK {
		t.Errorf("status = %d, want %d (default)", rr.status, http.StatusOK)
	}
}

func TestRequestLog_OneLinePerRequest(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))

	handler := requestLog(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("created"))
	}))

	req := httptest.NewRequest(http.MethodPost, "/foo/bar", nil)
	req.Host = "sites.nyxhub.net"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("got %d log lines, want 1; buf:\n%s", len(lines), buf.String())
	}

	var entry map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("log line is not valid JSON: %v; line: %s", err, lines[0])
	}

	if entry["msg"] != "request" {
		t.Errorf("msg = %v, want %q", entry["msg"], "request")
	}
	for _, key := range []string{"host", "path", "method", "status", "dur_ms", "bytes"} {
		if _, ok := entry[key]; !ok {
			t.Errorf("log line missing key %q: %v", key, entry)
		}
	}
	if entry["host"] != "sites.nyxhub.net" {
		t.Errorf("host = %v, want %q", entry["host"], "sites.nyxhub.net")
	}
	if entry["path"] != "/foo/bar" {
		t.Errorf("path = %v, want %q", entry["path"], "/foo/bar")
	}
	if entry["method"] != http.MethodPost {
		t.Errorf("method = %v, want %q", entry["method"], http.MethodPost)
	}
	if got, want := entry["status"], float64(http.StatusCreated); got != want {
		t.Errorf("status = %v, want %v", got, want)
	}
	if got, want := entry["bytes"], float64(len("created")); got != want {
		t.Errorf("bytes = %v, want %v", got, want)
	}
}

func TestInstrument_ObservesByClass(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := metrics.New(reg)
	cfg := &config.Config{BaseDomain: "sites.nyxhub.net"}

	handler := instrument(m, cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "sites.nyxhub.net"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := testutil.CollectAndCount(m.HTTPDuration); got != 1 {
		t.Errorf("CollectAndCount(HTTPDuration) = %d, want 1", got)
	}

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}
	var sawAppClass bool
	for _, f := range families {
		if f.GetName() != "http_request_duration_seconds" {
			continue
		}
		for _, metric := range f.GetMetric() {
			for _, label := range metric.GetLabel() {
				if label.GetName() == "class" && label.GetValue() == "app" {
					sawAppClass = true
				}
			}
		}
	}
	if !sawAppClass {
		t.Error("expected an observation labeled class=app")
	}
}
