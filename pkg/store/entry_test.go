package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
)

func mustSession(t *testing.T, db *DB) *Session {
	t.Helper()
	s, err := db.CreateSession("/tmp/x")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAppendEntry(t *testing.T) {
	db := openTestDB(t)
	s := mustSession(t, db)

	data := MessageData{Role: "user", Message: json.RawMessage(`{"role":"user"}`)}
	e, err := db.AppendEntry(s.ID, "", EntryTypeMessage, data)
	if err != nil {
		t.Fatal(err)
	}
	if e.ID == "" {
		t.Error("id missing")
	}
	if e.Type != EntryTypeMessage {
		t.Errorf("type = %q", e.Type)
	}
	if e.Timestamp == 0 {
		t.Error("timestamp not set")
	}
	if len(e.Data) == 0 {
		t.Error("data not encoded")
	}
}

func TestAppendEntryRawMessage(t *testing.T) {
	db := openTestDB(t)
	s := mustSession(t, db)

	raw := json.RawMessage(`{"custom":"payload"}`)
	e, err := db.AppendEntry(s.ID, "", EntryTypeCustom, raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(e.Data) != string(raw) {
		t.Errorf("data = %s", string(e.Data))
	}
}

func TestGetEntry(t *testing.T) {
	db := openTestDB(t)
	s := mustSession(t, db)
	e, _ := db.AppendEntry(s.ID, "", EntryTypeMessage, map[string]any{"k": "v"})

	got, err := db.GetEntry(e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != e.ID || got.Type != e.Type {
		t.Errorf("got %+v", got)
	}
}

func TestGetEntryMissing(t *testing.T) {
	db := openTestDB(t)
	_, err := db.GetEntry("missing")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("err = %v", err)
	}
}

func TestGetEntries(t *testing.T) {
	db := openTestDB(t)
	s := mustSession(t, db)

	for i := 0; i < 5; i++ {
		_, err := db.AppendEntry(s.ID, "", EntryTypeMessage, map[string]any{"i": i})
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := db.GetEntries(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Errorf("len = %d", len(got))
	}
	// Ordered by timestamp ascending.
	for i := 1; i < len(got); i++ {
		if got[i].Timestamp < got[i-1].Timestamp {
			t.Errorf("not ordered: %d vs %d", got[i].Timestamp, got[i-1].Timestamp)
		}
	}
}

func TestGetChildren(t *testing.T) {
	db := openTestDB(t)
	s := mustSession(t, db)
	parent, _ := db.AppendEntry(s.ID, "", EntryTypeMessage, nil)
	c1, _ := db.AppendEntry(s.ID, parent.ID, EntryTypeMessage, nil)
	c2, _ := db.AppendEntry(s.ID, parent.ID, EntryTypeMessage, nil)
	_, _ = db.AppendEntry(s.ID, "", EntryTypeMessage, nil) // unrelated root

	children, err := db.GetChildren(parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 2 {
		t.Fatalf("len = %d", len(children))
	}
	ids := map[string]bool{children[0].ID: true, children[1].ID: true}
	if !ids[c1.ID] || !ids[c2.ID] {
		t.Errorf("children = %+v", children)
	}
}

func TestGetBranchLinear(t *testing.T) {
	db := openTestDB(t)
	s := mustSession(t, db)
	root, _ := db.AppendEntry(s.ID, "", EntryTypeMessage, map[string]any{"n": 0})
	mid, _ := db.AppendEntry(s.ID, root.ID, EntryTypeMessage, map[string]any{"n": 1})
	leaf, _ := db.AppendEntry(s.ID, mid.ID, EntryTypeMessage, map[string]any{"n": 2})

	branch, err := db.GetBranch(leaf.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(branch) != 3 {
		t.Fatalf("len = %d", len(branch))
	}
	if branch[0].ID != root.ID || branch[1].ID != mid.ID || branch[2].ID != leaf.ID {
		t.Errorf("order = %v", []string{branch[0].ID, branch[1].ID, branch[2].ID})
	}
}

func TestGetBranchBranchedTree(t *testing.T) {
	db := openTestDB(t)
	s := mustSession(t, db)
	// root -> a -> a1
	//      -> b -> b1
	root, _ := db.AppendEntry(s.ID, "", EntryTypeMessage, nil)
	a, _ := db.AppendEntry(s.ID, root.ID, EntryTypeMessage, nil)
	a1, _ := db.AppendEntry(s.ID, a.ID, EntryTypeMessage, nil)
	b, _ := db.AppendEntry(s.ID, root.ID, EntryTypeMessage, nil)
	b1, _ := db.AppendEntry(s.ID, b.ID, EntryTypeMessage, nil)

	branch, _ := db.GetBranch(a1.ID)
	if len(branch) != 3 || branch[1].ID != a.ID {
		t.Errorf("branch a = %+v", branch)
	}

	branch2, _ := db.GetBranch(b1.ID)
	if len(branch2) != 3 || branch2[1].ID != b.ID {
		t.Errorf("branch b = %+v", branch2)
	}
}

func TestGetLeaves(t *testing.T) {
	db := openTestDB(t)
	s := mustSession(t, db)
	root, _ := db.AppendEntry(s.ID, "", EntryTypeMessage, nil)
	a, _ := db.AppendEntry(s.ID, root.ID, EntryTypeMessage, nil)
	b, _ := db.AppendEntry(s.ID, root.ID, EntryTypeMessage, nil)
	_, _ = a, b

	leaves, err := db.GetLeaves(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(leaves) != 2 {
		t.Errorf("leaves = %d", len(leaves))
	}
	for _, leaf := range leaves {
		if leaf.ID == root.ID {
			t.Errorf("root should not be a leaf")
		}
	}
}

func TestGetBranchEntriesDivergent(t *testing.T) {
	db := openTestDB(t)
	s := mustSession(t, db)
	// root -> shared -> A -> A1
	//                -> B -> B1
	root, _ := db.AppendEntry(s.ID, "", EntryTypeMessage, nil)
	shared, _ := db.AppendEntry(s.ID, root.ID, EntryTypeMessage, nil)
	a, _ := db.AppendEntry(s.ID, shared.ID, EntryTypeMessage, nil)
	a1, _ := db.AppendEntry(s.ID, a.ID, EntryTypeMessage, nil)
	b, _ := db.AppendEntry(s.ID, shared.ID, EntryTypeMessage, nil)
	b1, _ := db.AppendEntry(s.ID, b.ID, EntryTypeMessage, nil)

	// Divergent portion of A branch (A1) compared to B branch (B1).
	got, err := db.GetBranchEntries(a1.ID, b1.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Should contain A and A1, not root or shared.
	if len(got) != 2 {
		t.Fatalf("got %d, want 2: %+v", len(got), got)
	}
	ids := map[string]bool{got[0].ID: true, got[1].ID: true}
	if !ids[a.ID] || !ids[a1.ID] {
		t.Errorf("divergent = %+v", got)
	}
}

func TestDeleteSessionCascadesToEntries(t *testing.T) {
	db := openTestDB(t)
	s := mustSession(t, db)
	_, _ = db.AppendEntry(s.ID, "", EntryTypeMessage, nil)
	_, _ = db.AppendEntry(s.ID, "", EntryTypeMessage, nil)

	if err := db.DeleteSession(s.ID); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetEntries(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("entries not cascaded, %d remain", len(got))
	}
}
