package store

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

func rawDB(t *testing.T, dir string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(dir, "drop.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func scalar[T any](t *testing.T, db *sql.DB, query string) T {
	t.Helper()
	var v T
	if err := db.QueryRow(query).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestOpen_SchemaAndPragmas(t *testing.T) {
	// URI delimiters in DATA_DIR must remain part of the path.
	dir := filepath.Join(t.TempDir(), "data ?#%")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	d := openStore(t, dir)
	if _, err := os.Stat(filepath.Join(dir, "drop.db")); err != nil {
		t.Fatal(err)
	}
	if got := d.db.Stats().MaxOpenConnections; got != 1 {
		t.Errorf("max open connections = %d, want 1", got)
	}
	if got := scalar[string](t, d.db, "PRAGMA journal_mode"); got != "wal" {
		t.Errorf("journal_mode = %q, want wal", got)
	}
	for pragma, want := range map[string]int{"user_version": 1, "foreign_keys": 1, "busy_timeout": 5000, "synchronous": 1} {
		if got := scalar[int](t, d.db, "PRAGMA "+pragma); got != want {
			t.Errorf("%s = %d, want %d", pragma, got, want)
		}
	}
	cols := scalar[string](t, d.db, "SELECT group_concat(name, ',') FROM (SELECT name FROM pragma_table_info('sites') ORDER BY cid)")
	want := []string{"id", "created_at", "updated_at", "expires_at", "permanent", "size_bytes", "file_count", "source"}
	if !slices.Equal(strings.Split(cols, ","), want) {
		t.Errorf("columns = %s, want %v", cols, want)
	}
	if got := scalar[string](t, d.db, "SELECT group_concat(name, ',') FROM (SELECT name FROM pragma_index_info('idx_sites_reap') ORDER BY seqno)"); got != "permanent,expires_at" {
		t.Errorf("reap index = %q, want permanent,expires_at", got)
	}
	if _, err := d.db.Exec(`INSERT INTO sites VALUES ('bad', '', '', '', 2, 0, 0, 'api')`); err == nil {
		t.Fatal("invalid permanent value was accepted")
	}
}

func TestOpen_ReopenPreservesRows(t *testing.T) {
	dir := t.TempDir()
	d := openStore(t, dir)
	want := fixture("kept")
	seed(t, d, want)
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	d = openStore(t, dir)
	if got := scalar[int](t, d.db, "PRAGMA user_version"); got != 1 {
		t.Errorf("version after reopen = %d, want 1", got)
	}
	assertSite(t, d, want)
}

func TestOpen_RefusesFutureSchema(t *testing.T) {
	dir := t.TempDir()
	raw := rawDB(t, dir)
	if _, err := raw.Exec("PRAGMA user_version = 999"); err != nil {
		t.Fatal(err)
	}
	d, err := Open(dir)
	if d != nil {
		d.Close()
		t.Error("Open returned a DB for a future schema")
	}
	if !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("Open = %v, want ErrSchemaTooNew", err)
	}
	if got := scalar[int](t, raw, "PRAGMA user_version"); got != 999 {
		t.Errorf("future version modified to %d", got)
	}
	if got := scalar[int](t, raw, "SELECT count(*) FROM sqlite_schema WHERE name = 'sites'"); got != 0 {
		t.Errorf("Open applied a migration to a future DB")
	}
}

func TestMigrate_FailureRollsBackOnlyCurrentFile(t *testing.T) {
	db := rawDB(t, t.TempDir())
	files := fstest.MapFS{
		"migrations/0001_first.sql":  {Data: []byte("CREATE TABLE first (id INTEGER);")},
		"migrations/0002_second.sql": {Data: []byte("CREATE TABLE second (id INTEGER); PRAGMA user_version = 2; INSERT INTO missing VALUES (1);")},
	}
	if err := migrate(db, files); err == nil || !strings.Contains(err.Error(), "no such table: missing") {
		t.Fatalf("migrate = %v, want second migration failure", err)
	}
	if got := scalar[int](t, db, "PRAGMA user_version"); got != 1 {
		t.Errorf("version after failure = %d, want 1", got)
	}
	if got := scalar[string](t, db, "SELECT group_concat(name) FROM sqlite_schema WHERE type = 'table'"); got != "first" {
		t.Errorf("tables after failure = %q, want only first", got)
	}
	files["migrations/0002_second.sql"].Data = []byte("CREATE TABLE second (id INTEGER);")
	if err := migrate(db, files); err != nil {
		t.Fatalf("retry migration: %v", err)
	}
	if got := scalar[int](t, db, "PRAGMA user_version"); got != 2 {
		t.Errorf("version after retry = %d, want 2", got)
	}
}

func TestMigrate_RejectsMisnumberedFiles(t *testing.T) {
	db := rawDB(t, t.TempDir())
	for _, name := range []string{"junk.sql", "0002_gap.sql", "00001_extra_digit.sql"} {
		files := fstest.MapFS{"migrations/" + name: {Data: []byte("CREATE TABLE accidental (id INTEGER);")}}
		if err := migrate(db, files); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("migrate with %q = %v, want filename error", name, err)
		}
	}
}
