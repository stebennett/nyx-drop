package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestNew_RegistersCollectors(t *testing.T) {
	reg := prometheus.NewRegistry()
	New(reg)

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	var haveGoGoroutines, haveProcess bool
	for _, f := range families {
		switch {
		case f.GetName() == "go_goroutines":
			haveGoGoroutines = true
		case strings.HasPrefix(f.GetName(), "process_"):
			haveProcess = true
		}
	}
	if !haveGoGoroutines {
		t.Error("expected go_goroutines metric to be registered")
	}
	if !haveProcess {
		t.Error("expected a process_* metric to be registered")
	}
}

func TestHTTPDuration_Observes(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := New(reg)

	m.HTTPDuration.WithLabelValues("app").Observe(0.042)

	if got := testutil.CollectAndCount(m.HTTPDuration); got != 1 {
		t.Errorf("CollectAndCount(HTTPDuration) = %d, want 1", got)
	}
}
