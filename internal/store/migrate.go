package store

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"strings"
)

//go:embed migrations/*.sql
var migrations embed.FS

func migrate(db *sql.DB, files fs.FS) error {
	entries, err := fs.ReadDir(files, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	var names []string
	for _, entry := range entries { // ReadDir sorts by filename.
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		prefix, _, ok := strings.Cut(name, "_")
		if !ok || prefix != fmt.Sprintf("%04d", len(names)+1) {
			return fmt.Errorf("unexpected migration filename %q", name)
		}
		names = append(names, name)
	}

	var current int
	if err := db.QueryRow("PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if current > len(names) {
		return fmt.Errorf("%w (database version %d, supported version %d)", ErrSchemaTooNew, current, len(names))
	}
	for i, name := range names {
		version := i + 1
		if version <= current {
			continue
		}
		body, err := fs.ReadFile(files, path.Join("migrations", name))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		if err := applyMigration(db, version, string(body)); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
	}
	return nil
}

func applyMigration(db *sql.DB, version int, body string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(body); err != nil {
		return err
	}
	// SQLite's PRAGMA assignment does not accept a bound parameter. The
	// version is an integer derived solely from the embedded file order.
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
		return err
	}
	return tx.Commit()
}
