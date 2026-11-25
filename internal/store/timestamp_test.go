package store

import (
	"strings"
	"testing"
	"testing/quick"
	"time"
)

// Constrain to RFC 3339's four-digit year range near application dates.
func testTime(sec uint32, ns uint32) time.Time {
	return time.Unix(int64(sec), int64(ns%1_000_000_000)).In(time.FixedZone("offset", 19800))
}

func TestTimestamp_RoundTrip(t *testing.T) {
	if err := quick.Check(func(sec, ns uint32) bool {
		want := testTime(sec, ns)
		got, err := time.Parse(time.RFC3339Nano, encodeTime(want))
		return err == nil && got.Equal(want) && got.Location() == time.UTC
	}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestTimestamp_OrderMatchesChronology(t *testing.T) {
	if err := quick.Check(func(sec, an, bn uint32) bool {
		a, b := testTime(sec, an), testTime(sec, bn)
		return a.Compare(b) == strings.Compare(encodeTime(a), encodeTime(b))
	}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestGet_RejectsInvalidStoredTimestamps(t *testing.T) {
	d := openStore(t, t.TempDir())
	for _, tc := range []struct{ id, created, updated, expires string }{
		{"created", "bad", encodeTime(base), encodeTime(base)},
		{"updated", encodeTime(base), "bad", encodeTime(base)},
		{"expires", encodeTime(base), encodeTime(base), "bad"},
	} {
		if _, err := d.db.Exec(`INSERT INTO sites VALUES (?, ?, ?, ?, 0, 0, 0, 'api')`, tc.id, tc.created, tc.updated, tc.expires); err != nil {
			t.Fatal(err)
		}
		if _, err := d.Get(t.Context(), tc.id); err == nil || !strings.Contains(err.Error(), tc.id+"_at") {
			t.Errorf("Get(%s) = %v, want timestamp parse error", tc.id, err)
		}
	}
	if _, _, err := d.List(t.Context(), ListParams{Sort: "name"}); err == nil {
		t.Error("List accepted corrupted timestamps")
	}
}
