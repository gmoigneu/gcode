package store

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestCreateSession(t *testing.T) {
	db := openTestDB(t)
	s, err := db.CreateSession("/tmp/test")
	if err != nil {
		t.Fatal(err)
	}
	if s.ID == "" {
		t.Error("id not generated")
	}
	if s.Cwd != "/tmp/test" {
		t.Errorf("cwd = %q", s.Cwd)
	}
	if s.Version != 3 {
		t.Errorf("version = %d", s.Version)
	}
	if time.Since(time.UnixMilli(s.CreatedAt)) > time.Minute {
		t.Errorf("createdAt = %d", s.CreatedAt)
	}
}

func TestGetSession(t *testing.T) {
	db := openTestDB(t)
	created, _ := db.CreateSession("/home/user")

	got, err := db.GetSession(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != created.ID || got.Cwd != "/home/user" {
		t.Errorf("got %+v", got)
	}
}

func TestGetSessionMissing(t *testing.T) {
	db := openTestDB(t)
	_, err := db.GetSession("does-not-exist")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("err = %v", err)
	}
}

func TestListSessionsOrderedByCreatedAtDesc(t *testing.T) {
	db := openTestDB(t)

	first, _ := db.CreateSession("/a")
	time.Sleep(2 * time.Millisecond)
	second, _ := db.CreateSession("/b")
	time.Sleep(2 * time.Millisecond)
	third, _ := db.CreateSession("/c")

	got, err := db.ListSessions(ListSessionsOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].ID != third.ID || got[2].ID != first.ID {
		t.Errorf("order = %v %v %v", got[0].ID, got[1].ID, got[2].ID)
	}
	_ = second
}

func TestListSessionsCwdFilter(t *testing.T) {
	db := openTestDB(t)
	db.CreateSession("/a")
	db.CreateSession("/b")
	db.CreateSession("/a")

	got, err := db.ListSessions(ListSessionsOpts{Cwd: "/a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d", len(got))
	}
	for _, s := range got {
		if s.Cwd != "/a" {
			t.Errorf("cwd = %q", s.Cwd)
		}
	}
}

func TestListSessionsLimit(t *testing.T) {
	db := openTestDB(t)
	for i := 0; i < 5; i++ {
		db.CreateSession("/x")
		time.Sleep(1 * time.Millisecond)
	}
	got, err := db.ListSessions(ListSessionsOpts{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d", len(got))
	}
}

func TestUpdateSessionName(t *testing.T) {
	db := openTestDB(t)
	s, _ := db.CreateSession("/x")

	if err := db.UpdateSessionName(s.ID, "renamed"); err != nil {
		t.Fatal(err)
	}
	got, _ := db.GetSession(s.ID)
	if got.Name != "renamed" {
		t.Errorf("name = %q", got.Name)
	}
}

func TestUpdateSessionNameMissing(t *testing.T) {
	db := openTestDB(t)
	if err := db.UpdateSessionName("missing", "x"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("err = %v", err)
	}
}

func TestDeleteSession(t *testing.T) {
	db := openTestDB(t)
	s, _ := db.CreateSession("/x")

	if err := db.DeleteSession(s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetSession(s.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestDeleteSessionMissing(t *testing.T) {
	db := openTestDB(t)
	if err := db.DeleteSession("missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("err = %v", err)
	}
}

func TestNewEntryIDDistinct(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := NewEntryID()
		if len(id) != 8 {
			t.Errorf("id = %q", id)
		}
		if seen[id] {
			t.Errorf("duplicate id: %q", id)
		}
		seen[id] = true
	}
}
