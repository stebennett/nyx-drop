// Package store owns SQLite persistence for site records.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

var (
	ErrNotFound     = errors.New("store: site not found")
	ErrDuplicate    = errors.New("store: duplicate site id")
	ErrSchemaTooNew = errors.New("store: database schema is newer than this binary")
)

// Site is the persisted metadata for an uploaded site.
type Site struct {
	ID        string
	CreatedAt time.Time
	UpdatedAt time.Time
	ExpiresAt time.Time
	Permanent bool
	SizeBytes int64
	FileCount int
	Source    string // "api" or "web"
}

// DB is a SQLite-backed store. It is safe for concurrent use.
type DB struct {
	db *sql.DB
}

// Open opens drop.db in an existing data directory and applies pending migrations.
func Open(dataDir string) (*DB, error) {
	path, err := filepath.Abs(filepath.Join(dataDir, "drop.db"))
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	dsn := url.URL{
		Scheme:   "file",
		Path:     filepath.ToSlash(path),
		RawQuery: "_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)",
	}
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, fmt.Errorf("open SQLite: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping SQLite: %w", err)
	}
	if err := migrate(db, migrations); err != nil {
		db.Close()
		return nil, err
	}
	return &DB{db: db}, nil
}

func (d *DB) Close() error { return d.db.Close() }

func (d *DB) Ping(ctx context.Context) error { return d.db.PingContext(ctx) }

const siteColumns = "id, created_at, updated_at, expires_at, permanent, size_bytes, file_count, source"

// Create inserts a temporary site, regardless of s.Permanent.
func (d *DB) Create(ctx context.Context, s Site) error {
	_, err := d.db.ExecContext(ctx, `INSERT INTO sites (`+siteColumns+`) VALUES (?, ?, ?, ?, 0, ?, ?, ?)`,
		s.ID, encodeTime(s.CreatedAt), encodeTime(s.UpdatedAt), encodeTime(s.ExpiresAt), s.SizeBytes, s.FileCount, s.Source)
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY {
		return ErrDuplicate
	}
	return err
}

func (d *DB) Get(ctx context.Context, id string) (Site, error) {
	s, err := scanSite(d.db.QueryRowContext(ctx, `SELECT `+siteColumns+` FROM sites WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Site{}, ErrNotFound
	}
	return s, err
}

func (d *DB) Touch(ctx context.Context, id string, updatedAt, expiresAt time.Time, size int64, files int) error {
	return changed(d.db.ExecContext(ctx, `UPDATE sites SET updated_at = ?, expires_at = ?, size_bytes = ?, file_count = ? WHERE id = ?`,
		encodeTime(updatedAt), encodeTime(expiresAt), size, files, id))
}

// SetPermanent preserves the stored expiry when v is true, and replaces it
// with newExpiry when v is false.
func (d *DB) SetPermanent(ctx context.Context, id string, v bool, newExpiry time.Time) error {
	if v {
		return changed(d.db.ExecContext(ctx, `UPDATE sites SET permanent = 1 WHERE id = ?`, id))
	}
	return changed(d.db.ExecContext(ctx, `UPDATE sites SET permanent = 0, expires_at = ? WHERE id = ?`, encodeTime(newExpiry), id))
}

func (d *DB) Delete(ctx context.Context, id string) error {
	return changed(d.db.ExecContext(ctx, `DELETE FROM sites WHERE id = ?`, id))
}

func changed(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err == nil && rows == 0 {
		return ErrNotFound
	}
	return err
}

// Expired returns nonpermanent site IDs whose expiry is at or before now.
func (d *DB) Expired(ctx context.Context, now time.Time) ([]string, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT id FROM sites WHERE permanent = 0 AND expires_at <= ?`, encodeTime(now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Counts returns counts of all persisted rows and the permanent subset.
func (d *DB) Counts(ctx context.Context) (active, permanent int, err error) {
	err = d.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(permanent), 0) FROM sites`).Scan(&active, &permanent)
	return
}

// Fixed-width fractional seconds preserve both precision and lexical order.
const timeLayout = "2006-01-02T15:04:05.000000000Z07:00"

func encodeTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func scanSite(row interface{ Scan(...any) error }) (Site, error) {
	var s Site
	var created, updated, expires string
	if err := row.Scan(&s.ID, &created, &updated, &expires, &s.Permanent, &s.SizeBytes, &s.FileCount, &s.Source); err != nil {
		return Site{}, err
	}
	for _, ts := range []struct {
		column, text string
		dest         *time.Time
	}{
		{"created_at", created, &s.CreatedAt},
		{"updated_at", updated, &s.UpdatedAt},
		{"expires_at", expires, &s.ExpiresAt},
	} {
		t, err := time.Parse(time.RFC3339Nano, ts.text)
		if err != nil {
			return Site{}, fmt.Errorf("parse %s: %w", ts.column, err)
		}
		*ts.dest = t.UTC()
	}
	return s, nil
}
