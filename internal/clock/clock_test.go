package clock

import (
	"testing"
	"time"
)

func TestReal_NowIsUTC(t *testing.T) {
	r := Real{}
	now := r.Now()
	if now.Location() != time.UTC {
		t.Fatalf("Real.Now() location = %v, want UTC", now.Location())
	}
}

func TestFake_AdvanceAndSet(t *testing.T) {
	start := time.Date(2026, 7, 9, 12, 0, 0, 0, time.FixedZone("EST", -5*60*60))
	f := NewFake(start)

	// Now() is idempotent between calls (no implicit advance).
	first := f.Now()
	second := f.Now()
	if !first.Equal(second) {
		t.Fatalf("Now() not idempotent: %v != %v", first, second)
	}
	if first.Location() != time.UTC {
		t.Fatalf("NewFake did not normalize to UTC: location = %v", first.Location())
	}
	wantStart := start.UTC()
	if !first.Equal(wantStart) {
		t.Fatalf("Now() = %v, want %v", first, wantStart)
	}

	f.Advance(2 * time.Hour)
	if got := f.Now(); !got.Equal(wantStart.Add(2 * time.Hour)) {
		t.Fatalf("after Advance(2h): Now() = %v, want %v", got, wantStart.Add(2*time.Hour))
	}

	newTime := time.Date(2030, 1, 1, 0, 0, 0, 0, time.FixedZone("PST", -8*60*60))
	f.SetNow(newTime)
	if got := f.Now(); !got.Equal(newTime.UTC()) {
		t.Fatalf("after SetNow: Now() = %v, want %v", got, newTime.UTC())
	}
	if f.Now().Location() != time.UTC {
		t.Fatalf("SetNow did not normalize to UTC: location = %v", f.Now().Location())
	}
}
