package store

import (
	"database/sql"
	"fmt"
)

// migration is a single schema upgrade step.
type migration struct {
	version int
	sql     string
}

// migrations are applied in order. Never renumber or reorder entries — new
// schema changes must append a new migration.
var migrations = []migration{
	{version: 1, sql: migrationV1SQL},
}

const migrationV1SQL = `
CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
    id              TEXT PRIMARY KEY,
    version         INTEGER NOT NULL DEFAULT 3,
    cwd             TEXT NOT NULL,
    parent_session  TEXT,
    created_at      INTEGER NOT NULL,
    name            TEXT
);

CREATE TABLE IF NOT EXISTS entries (
    id          TEXT PRIMARY KEY,
    session_id  TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    parent_id   TEXT,
    type        TEXT NOT NULL,
    timestamp   INTEGER NOT NULL,
    data        BLOB NOT NULL,
    FOREIGN KEY (parent_id) REFERENCES entries(id)
);

CREATE INDEX IF NOT EXISTS idx_entries_session ON entries(session_id);
CREATE INDEX IF NOT EXISTS idx_entries_parent  ON entries(parent_id);
CREATE INDEX IF NOT EXISTS idx_entries_type    ON entries(session_id, type);
`

// migrate brings the database up to the latest schema version. It is
// idempotent: schema_version gates already-applied migrations.
func (d *DB) migrate() error {
	if _, err := d.db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`); err != nil {
		return fmt.Errorf("store: migrate: create schema_version: %w", err)
	}

	current, err := d.currentSchemaVersion()
	if err != nil {
		return err
	}

	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		tx, err := d.db.Begin()
		if err != nil {
			return fmt.Errorf("store: migrate: begin tx: %w", err)
		}
		if _, err := tx.Exec(m.sql); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: migrate: v%d: %w", m.version, err)
		}
		if err := setSchemaVersion(tx, m.version); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("store: migrate: commit v%d: %w", m.version, err)
		}
		current = m.version
	}
	return nil
}

func (d *DB) currentSchemaVersion() (int, error) {
	var version int
	err := d.db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&version)
	if err != nil && err != sql.ErrNoRows {
		return 0, fmt.Errorf("store: read schema_version: %w", err)
	}
	return version, nil
}

func setSchemaVersion(tx *sql.Tx, version int) error {
	// Upsert pattern: delete any existing row, then insert the new version.
	if _, err := tx.Exec(`DELETE FROM schema_version`); err != nil {
		return fmt.Errorf("store: clear schema_version: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO schema_version(version) VALUES (?)`, version); err != nil {
		return fmt.Errorf("store: write schema_version: %w", err)
	}
	return nil
}
