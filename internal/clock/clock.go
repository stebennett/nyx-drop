// Package clock provides an injectable time source so handlers and
// business logic never call time.Now() directly (invariant 9): tests use
// Fake, production uses Real. Now() always returns UTC.
package clock

import (
	"sync"
	"time"
)

// Clock is the injectable time source. Now always returns a UTC time.
type Clock interface {
	Now() time.Time
}

// Real is the production Clock, backed by time.Now().
type Real struct{}

// Now returns the current time in UTC.
func (Real) Now() time.Time {
	return time.Now().UTC()
}

// Fake is a controllable Clock for tests.
type Fake struct {
	mu sync.Mutex
	t  time.Time
}

// NewFake returns a Fake initialized to t, normalized to UTC.
func NewFake(t time.Time) *Fake {
	return &Fake{t: t.UTC()}
}

// Now returns the fake's current time in UTC.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

// SetNow sets the fake's current time, normalized to UTC.
func (f *Fake) SetNow(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.t = t.UTC()
}

// Advance moves the fake's current time forward by d.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.t = f.t.Add(d)
}
