package main

import (
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRun_StoreOpenError(t *testing.T) {
	// Reserve the configured port so skipping store.Open fails promptly
	// with a listen error instead of blocking in run.
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	env := map[string]string{
		"BASE_DOMAIN":          "test.local",
		"UPLOAD_TOKEN":         "test",
		"GITHUB_CLIENT_ID":     "test",
		"GITHUB_CLIENT_SECRET": "test",
		"ADMIN_GITHUB_USER":    "test",
		"SESSION_SECRET":       "test",
		"DATA_DIR":             filepath.Join(t.TempDir(), "missing"),
		"PORT":                 strconv.Itoa(listener.Addr().(*net.TCPAddr).Port),
	}
	err = run(func(key string) string { return env[key] })
	if err == nil || !strings.Contains(err.Error(), "store: ping") {
		t.Fatalf("run() = %v, want store startup error", err)
	}
}

// TestNewHTTPServer_SetsTimeouts guards against the connection-hold
// resource-exhaustion vector where idle keep-alive connections are never
// reaped: IdleTimeout must be non-zero and must not silently inherit an
// unset ReadTimeout (Go's http.Server falls back IdleTimeout->ReadTimeout
// when IdleTimeout is zero, and both were zero here previously).
// WriteTimeout is deliberately left unset (0 == no limit): CARD-003/004
// add real site/upload serving whose response sizes are bounded by
// MAX_SITE_SIZE/MAX_UPLOAD_SIZE, not yet known to this wiring, so a fixed
// WriteTimeout here risks truncating large future responses.
func TestNewHTTPServer_SetsTimeouts(t *testing.T) {
	handler := http.NewServeMux()
	srv := newHTTPServer(":8080", handler)

	if srv.Addr != ":8080" {
		t.Errorf("Addr = %q, want :8080", srv.Addr)
	}
	if srv.Handler != http.Handler(handler) {
		t.Errorf("Handler not wired through")
	}
	if srv.ReadHeaderTimeout != 10*time.Second {
		t.Errorf("ReadHeaderTimeout = %v, want 10s", srv.ReadHeaderTimeout)
	}
	if srv.IdleTimeout <= 0 {
		t.Errorf("IdleTimeout = %v, want > 0 (idle keep-alive connections must be reaped)", srv.IdleTimeout)
	}
	if srv.WriteTimeout != 0 {
		t.Errorf("WriteTimeout = %v, want 0 (unset until upload/site size limits are wired, CARD-003/004)", srv.WriteTimeout)
	}
}
