package lock

import (
	"runtime"
	"testing"
	"time"
)

func TestSites(t *testing.T) {
	var sites Sites
	unlock := sites.Lock("a")
	same, other := make(chan struct{}), make(chan struct{})
	go func() {
		release := sites.Lock("a")
		release()
		close(same)
	}()
	go func() {
		release := sites.Lock("b")
		release()
		close(other)
	}()
	defer func() {
		if unlock != nil {
			unlock()
		}
	}()
	wait := func(done chan struct{}) {
		t.Helper()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("site lock did not release")
		}
	}
	wait(other)
	deadline := time.Now().Add(3 * time.Second)
	for {
		sites.mu.Lock()
		refs := sites.entries["a"].refs
		sites.mu.Unlock()
		if refs == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("same-site waiter never queued")
		}
		runtime.Gosched()
	}
	select {
	case <-same:
		t.Fatal("two callers entered the same keyed critical section")
	default:
	}
	unlock()
	unlock = nil
	wait(same)
	if len(sites.entries) != 0 {
		t.Fatalf("locks retained without holders or waiters: %v", sites.entries)
	}
}
