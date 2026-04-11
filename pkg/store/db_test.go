package store

import (
	"path/filepath"
	"testing"
)

func TestOpenMemory(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Verify the sessions table exists.
	_, err = db.SQL().Exec(`SELECT COUNT(*) FROM sessions`)
	if err != nil {
		t.Errorf("sessions table missing: %v", err)
	}
}

func TestOpenCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	// Reopen — should succeed without re-running migrations from scratch.
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db2.Close()

	version, err := db2.currentSchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if version != len(migrations) {
		t.Errorf("version = %d, want %d", version, len(migrations))
	}
}

func TestMigrationIdempotent(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Running migrate again must be a no-op.
	if err := db.migrate(); err != nil {
		t.Errorf("second migrate failed: %v", err)
	}
	version, err := db.currentSchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if version != len(migrations) {
		t.Errorf("version = %d", version)
	}
}

func TestJournalModeWAL(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "wal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var mode string
	if err := db.SQL().QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
}

func TestForeignKeysEnabled(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var fk int
	if err := db.SQL().QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil {
		t.Fatal(err)
	}
	if fk != 1 {
		t.Errorf("foreign_keys = %d, want 1", fk)
	}
}

func TestCloseSafeOnNil(t *testing.T) {
	var db *DB
	if err := db.Close(); err != nil {
		t.Errorf("nil Close should not error: %v", err)
	}
}
