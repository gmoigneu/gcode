package compaction

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gmoigneu/gcode/pkg/agent"
	"github.com/gmoigneu/gcode/pkg/ai"
	"github.com/gmoigneu/gcode/pkg/store"
)

// stubBranchDB is a canned branchDB for tests.
type stubBranchDB struct {
	entries []store.Entry
	err     error
}

func (s *stubBranchDB) GetBranchEntries(from, to string) ([]store.Entry, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.entries, nil
}

func msgBranchEntry(t *testing.T, id, role, text string) store.Entry {
	t.Helper()
	var msg agent.AgentMessage
	switch role {
	case "user":
		msg = &ai.UserMessage{Content: []ai.Content{&ai.TextContent{Text: text}}}
	case "assistant":
		msg = &ai.AssistantMessage{Content: []ai.Content{&ai.TextContent{Text: text}}}
	}
	md, _ := store.SerializeMessageEntry(msg)
	raw, _ := json.Marshal(md)
	return store.Entry{ID: id, Type: store.EntryTypeMessage, Data: raw}
}

func TestPrepareBranchSummaryCollectsMessages(t *testing.T) {
	entries := []store.Entry{
		msgBranchEntry(t, "e1", "user", "goal"),
		msgBranchEntry(t, "e2", "assistant", "working on it"),
		msgBranchEntry(t, "e3", "user", "any progress"),
	}
	db := &stubBranchDB{entries: entries}
	prep, err := PrepareBranchSummary(db, "leaf", "target", CompactionSettings{KeepRecentTokens: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if prep == nil {
		t.Fatal("prep nil")
	}
	if len(prep.Messages) != 3 {
		t.Errorf("messages = %d", len(prep.Messages))
	}
	if prep.FromID != "leaf" {
		t.Errorf("fromID = %q", prep.FromID)
	}
}

func TestPrepareBranchSummaryEmptyEntries(t *testing.T) {
	prep, err := PrepareBranchSummary(&stubBranchDB{}, "a", "b", CompactionSettings{KeepRecentTokens: 100})
	if err != nil {
		t.Fatal(err)
	}
	if prep != nil {
		t.Error("expected nil for empty entries")
	}
}

func TestPrepareBranchSummaryRespectsTokenBudget(t *testing.T) {
	// 10 messages, each ~50 token estimate. Budget 120 should collect the
	// most recent 3 or so.
	var entries []store.Entry
	for i := 0; i < 10; i++ {
		entries = append(entries, msgBranchEntry(t, "e"+string(rune('a'+i)), "user", strings.Repeat("x", 200)))
	}
	db := &stubBranchDB{entries: entries}
	prep, err := PrepareBranchSummary(db, "leaf", "target", CompactionSettings{KeepRecentTokens: 120})
	if err != nil {
		t.Fatal(err)
	}
	if prep == nil {
		t.Fatal("prep nil")
	}
	if len(prep.Messages) >= 10 {
		t.Errorf("budget ignored: collected %d", len(prep.Messages))
	}
	if len(prep.Messages) == 0 {
		t.Error("expected at least one message")
	}
}

func TestPrepareBranchSummaryNilDB(t *testing.T) {
	_, err := PrepareBranchSummary(nil, "a", "b", CompactionSettings{})
	if err == nil {
		t.Error("expected error for nil db")
	}
}

func TestSummarizeBranchPrependsPreamble(t *testing.T) {
	prep := &BranchSummaryPreparation{
		Messages: []agent.AgentMessage{
			&ai.UserMessage{Content: []ai.Content{&ai.TextContent{Text: "branch work"}}},
		},
		FileOps: NewFileOperations(),
	}
	result, err := SummarizeBranch(
		context.Background(), prep,
		testModel(), "",
		CompactionSettings{ReserveTokens: 1000},
		fauxStreamFunc("branch body"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(result.Summary, BranchSummaryPreamble) {
		t.Errorf("preamble missing: %q", result.Summary)
	}
	if !strings.Contains(result.Summary, "branch body") {
		t.Errorf("body missing: %q", result.Summary)
	}
}

func TestSummarizeBranchAppendsFileOps(t *testing.T) {
	ops := NewFileOperations()
	ops.Read["/branch.go"] = true
	ops.Edited["/touched.go"] = true

	prep := &BranchSummaryPreparation{
		Messages: []agent.AgentMessage{
			&ai.UserMessage{Content: []ai.Content{&ai.TextContent{Text: "x"}}},
		},
		FileOps: ops,
	}
	result, err := SummarizeBranch(
		context.Background(), prep,
		testModel(), "",
		CompactionSettings{ReserveTokens: 1000},
		fauxStreamFunc("body"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Summary, "<read-files>") || !strings.Contains(result.Summary, "/branch.go") {
		t.Errorf("read-files missing: %q", result.Summary)
	}
	if !strings.Contains(result.Summary, "<modified-files>") || !strings.Contains(result.Summary, "/touched.go") {
		t.Errorf("modified-files missing: %q", result.Summary)
	}
	if len(result.ReadFiles) != 1 || result.ReadFiles[0] != "/branch.go" {
		t.Errorf("ReadFiles = %v", result.ReadFiles)
	}
	if len(result.ModifiedFiles) != 1 || result.ModifiedFiles[0] != "/touched.go" {
		t.Errorf("ModifiedFiles = %v", result.ModifiedFiles)
	}
}

func TestSummarizeBranchEmptyPreparation(t *testing.T) {
	_, err := SummarizeBranch(
		context.Background(),
		&BranchSummaryPreparation{},
		testModel(), "",
		CompactionSettings{},
		fauxStreamFunc("ignored"),
	)
	if err == nil {
		t.Error("expected error")
	}
}
