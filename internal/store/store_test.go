package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

var base = time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

func fixture(id string) Site {
	return Site{ID: id, CreatedAt: base, UpdatedAt: base, ExpiresAt: base.Add(time.Hour), SizeBytes: 1024, FileCount: 2, Source: "api"}
}

func openStore(t testing.TB, dir string) *DB {
	t.Helper()
	d, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func seed(t testing.TB, d *DB, sites ...Site) {
	t.Helper()
	for _, site := range sites {
		if err := d.Create(t.Context(), site); err != nil {
			t.Fatalf("Create(%q): %v", site.ID, err)
		}
		if site.Permanent {
			if err := d.SetPermanent(t.Context(), site.ID, true, time.Time{}); err != nil {
				t.Fatalf("SetPermanent(%q): %v", site.ID, err)
			}
		}
	}
}

func assertSite(t *testing.T, d *DB, want Site) {
	t.Helper()
	got, err := d.Get(t.Context(), want.ID)
	if err != nil {
		t.Fatal(err)
	}
	want.CreatedAt, want.UpdatedAt, want.ExpiresAt = want.CreatedAt.UTC(), want.UpdatedAt.UTC(), want.ExpiresAt.UTC()
	if got != want {
		t.Errorf("site = %+v, want %+v", got, want)
	}
}

func TestCreateGet_RoundTripAndConstraints(t *testing.T) {
	d := openStore(t, t.TempDir())
	site := fixture("roundtrip")
	site.CreatedAt = base.Add(123456789).In(time.FixedZone("offset", -5*3600))
	site.UpdatedAt = site.CreatedAt
	site.ExpiresAt = site.CreatedAt.Add(time.Hour)
	site.SizeBytes = 5 << 30
	site.Source = "web"
	// New rows are always temporary; only SetPermanent toggles permanence.
	site.Permanent = true
	if err := d.Create(t.Context(), site); err != nil {
		t.Fatal(err)
	}
	site.Permanent = false
	assertSite(t, d, site)
	if err := d.Create(t.Context(), site); !errors.Is(err, ErrDuplicate) {
		t.Errorf("duplicate Create = %v, want ErrDuplicate", err)
	}
	assertSite(t, d, site)
	site.ID, site.Source = "invalid", "other"
	if err := d.Create(t.Context(), site); err == nil || errors.Is(err, ErrDuplicate) || !strings.Contains(err.Error(), "CHECK constraint failed") {
		t.Errorf("bad-source Create = %v, want CHECK constraint error", err)
	}
}

func TestTouchSetPermanentDelete(t *testing.T) {
	d := openStore(t, t.TempDir())
	site := fixture("mutable")
	seed(t, d, site)
	site.UpdatedAt = base.Add(time.Minute)
	site.ExpiresAt = site.ExpiresAt.Add(time.Minute)
	site.SizeBytes, site.FileCount = 99, 1
	if err := d.Touch(t.Context(), site.ID, site.UpdatedAt, site.ExpiresAt, site.SizeBytes, site.FileCount); err != nil {
		t.Fatal(err)
	}
	assertSite(t, d, site)
	if err := d.SetPermanent(t.Context(), site.ID, true, time.Time{}); err != nil {
		t.Fatal(err)
	}
	site.Permanent = true
	assertSite(t, d, site)
	site.ExpiresAt = base.Add(24 * time.Hour)
	if err := d.SetPermanent(t.Context(), site.ID, false, site.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	site.Permanent = false
	assertSite(t, d, site)
	if err := d.Delete(t.Context(), site.ID); err != nil {
		t.Fatal(err)
	}
	_, err := d.Get(t.Context(), site.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete = %v, want ErrNotFound", err)
	}
}

func TestMissingRows(t *testing.T) {
	d := openStore(t, t.TempDir())
	ctx := t.Context()
	const id = "missing"
	_, err := d.Get(ctx, id)
	for op, err := range map[string]error{
		"Get": err, "Touch": d.Touch(ctx, id, base, base, 0, 0), "Delete": d.Delete(ctx, id),
		"SetPermanent(true)": d.SetPermanent(ctx, id, true, base), "SetPermanent(false)": d.SetPermanent(ctx, id, false, base),
	} {
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("%s = %v, want ErrNotFound", op, err)
		}
	}
}

func TestExpiredAndCounts(t *testing.T) {
	d := openStore(t, t.TempDir())
	checkCounts := func(wantActive, wantPermanent int) {
		t.Helper()
		active, permanent, err := d.Counts(t.Context())
		if err != nil || active != wantActive || permanent != wantPermanent {
			t.Fatalf("Counts = (%d, %d, %v), want (%d, %d, nil)", active, permanent, err, wantActive, wantPermanent)
		}
	}
	checkCounts(0, 0)
	past, boundary, future, permanent := fixture("past"), fixture("boundary"), fixture("future"), fixture("permanent")
	past.ExpiresAt, boundary.ExpiresAt, future.ExpiresAt = base.Add(-1), base, base.Add(1)
	permanent.ExpiresAt, permanent.Permanent = base.Add(-time.Hour), true
	seed(t, d, past, boundary, future, permanent)
	checkCounts(4, 1)
	got, err := d.Expired(t.Context(), base.In(time.FixedZone("offset", 3600)))
	slices.Sort(got)
	if err != nil || !slices.Equal(got, []string{"boundary", "past"}) {
		t.Errorf("Expired = (%v, %v), want ([boundary past], nil)", got, err)
	}
}

func TestReadiness(t *testing.T) {
	dir := t.TempDir()
	d := openStore(t, dir)
	if err := Readiness(d, dir)(t.Context()); err != nil {
		t.Fatal(err)
	}
	if files, err := filepath.Glob(filepath.Join(dir, ".readyz-*")); err != nil || len(files) != 0 {
		t.Fatalf("readiness left probe files: (%v, %v)", files, err)
	}
	// An existing regular file is never a writable directory, even as root.
	if err := Readiness(d, filepath.Join(dir, "drop.db"))(t.Context()); err == nil || !strings.Contains(err.Error(), "data directory") {
		t.Fatalf("non-directory probe = %v, want disk error", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := Readiness(d, dir)(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled probe = %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if err := Readiness(d, dir)(t.Context()); err == nil || !strings.Contains(err.Error(), "ping") {
		t.Fatalf("closed DB probe = %v, want ping error", err)
	}
}

func TestOpen_MissingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	if d, err := Open(dir); err == nil {
		d.Close()
		t.Fatal("Open succeeded without its data directory")
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Open unexpectedly created DATA_DIR: %v", err)
	}
}

func TestOperations_PropagateCanceledContext(t *testing.T) {
	d := openStore(t, t.TempDir())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, getErr := d.Get(ctx, "id")
	_, _, listErr := d.List(ctx, ListParams{})
	_, expiryErr := d.Expired(ctx, base)
	_, _, countsErr := d.Counts(ctx)
	for op, err := range map[string]error{
		"Get": getErr, "List": listErr, "Expired": expiryErr, "Counts": countsErr,
		"Create": d.Create(ctx, fixture("id")), "Delete": d.Delete(ctx, "id"),
	} {
		if !errors.Is(err, context.Canceled) {
			t.Errorf("%s = %v, want context.Canceled", op, err)
		}
	}
}
