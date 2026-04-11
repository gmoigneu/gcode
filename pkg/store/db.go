package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// DB wraps a sql.DB configured for gcode's SQLite schema. All store
// operations go through a DB value.
type DB struct {
	db *sql.DB
}

// Open opens (or creates) a SQLite database at path. It enables WAL mode
// and foreign keys and runs pending migrations before returning. Use
// ":memory:" for ephemeral in-process databases (unit tests).
func Open(path string) (*DB, error) {
	handle, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}

	// modernc.org/sqlite does not maintain a connection pool that works
	// well with ephemeral :memory: databases when the pool exceeds one
	// connection. Keep things simple and effective.
	handle.SetMaxOpenConns(1)

	if _, err := handle.Exec("PRAGMA journal_mode=WAL"); err != nil {
		handle.Close()
		return nil, fmt.Errorf("store: enable WAL: %w", err)
	}
	if _, err := handle.Exec("PRAGMA foreign_keys=ON"); err != nil {
		handle.Close()
		return nil, fmt.Errorf("store: enable foreign keys: %w", err)
	}
	if _, err := handle.Exec("PRAGMA synchronous=NORMAL"); err != nil {
		handle.Close()
		return nil, fmt.Errorf("store: set synchronous: %w", err)
	}

	s := &DB{db: handle}
	if err := s.migrate(); err != nil {
		handle.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the underlying SQL handle.
func (d *DB) Close() error {
	if d == nil || d.db == nil {
		return nil
	}
	return d.db.Close()
}

// SQL exposes the underlying *sql.DB for packages that need to run ad-hoc
// queries (e.g. integration tests).
func (d *DB) SQL() *sql.DB { return d.db }
