package store

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// Readiness returns a probe that requires both a responsive DB and a writable
// data directory. Each probe removes its uniquely named temporary file.
func Readiness(d *DB, dataDir string) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := d.Ping(ctx); err != nil {
			return fmt.Errorf("ping SQLite: %w", err)
		}
		f, err := os.CreateTemp(dataDir, ".readyz-*")
		if err != nil {
			return fmt.Errorf("probe data directory: %w", err)
		}
		_, writeErr := f.WriteString("\n")
		if err := errors.Join(writeErr, f.Close(), os.Remove(f.Name())); err != nil {
			return fmt.Errorf("probe data directory: %w", err)
		}
		return nil
	}
}
