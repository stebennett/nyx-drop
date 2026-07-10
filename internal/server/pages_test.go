package server

import (
	"strings"
	"testing"

	"nyx-drop/internal/config"
)

func TestRenderNotFound_InjectsAppURL(t *testing.T) {
	cfg := &config.Config{Scheme: "https", BaseDomain: "sites.nyxhub.net"}

	got, err := renderNotFound(cfg)
	if err != nil {
		t.Fatalf("renderNotFound() error: %v", err)
	}

	body := string(got)
	if !strings.Contains(body, cfg.ExternalOrigin()) {
		t.Errorf("rendered body missing ExternalOrigin() %q; body:\n%s", cfg.ExternalOrigin(), body)
	}
	if !strings.Contains(body, "faded into the night") {
		t.Errorf("rendered body missing expected copy; body:\n%s", body)
	}
}
