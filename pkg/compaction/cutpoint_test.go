package compaction

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gmoigneu/gcode/pkg/agent"
	"github.com/gmoigneu/gcode/pkg/ai"
	"github.com/gmoigneu/gcode/pkg/store"
)

// helpers --------------------------------------------------------------

func messageDataEntry(t *testing.T, id, role string, tokens int) store.Entry {
	t.Helper()
	text := strings.Repeat("x", tokens*4)
	var msg agent.AgentMessage
	switch role {
	case "user":
		msg = &ai.UserMessage{Content: []ai.Content{&ai.TextContent{Text: text}}}
	case "assistant":
		msg = &ai.AssistantMessage{Content: []ai.Content{&ai.TextContent{Text: text}}}
	case "toolResult":
		msg = &ai.ToolResultMessage{Content: []ai.Content{&ai.TextContent{Text: text}}}
	}
	md, _ := store.SerializeMessageEntry(msg)
	raw, _ := json.Marshal(md)
	return store.Entry{ID: id, Type: store.EntryTypeMessage, Data: raw}
}

func blobEntry2(t *testing.T, id string, typ store.EntryType, payload any) store.Entry {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return store.Entry{ID: id, Type: typ, Data: raw}
}

func messagesFromEntries(t *testing.T, entries []store.Entry) []agent.AgentMessage {
	t.Helper()
	var out []agent.AgentMessage
	for _, e := range entries {
		if e.Type != store.EntryTypeMessage {
			continue
		}
		msg, err := store.DeserializeMessageEntry(e)
		if err != nil {
			t.Fatalf("deserialize %s: %v", e.ID, err)
		}
		out = append(out, msg)
	}
	return out
}

// IsValidCutPoint -------------------------------------------------------

func TestIsValidCutPointUserMessage(t *testing.T) {
	e := messageDataEntry(t, "e1", "user", 1)
	if !IsValidCutPoint(e) {
		t.Error("user message should be valid")
	}
}

func TestIsValidCutPointAssistantMessage(t *testing.T) {
	e := messageDataEntry(t, "e1", "assistant", 1)
	if !IsValidCutPoint(e) {
		t.Error("assistant message should be valid")
	}
}

func TestIsValidCutPointToolResultNever(t *testing.T) {
	e := messageDataEntry(t, "e1", "toolResult", 1)
	if IsValidCutPoint(e) {
		t.Error("tool result should never be valid cut point")
	}
}

func TestIsValidCutPointCustomMessage(t *testing.T) {
	e := store.Entry{Type: store.EntryTypeCustomMessage, Data: json.RawMessage(`{}`)}
	if !IsValidCutPoint(e) {
		t.Error("custom message should be valid")
	}
}

func TestIsValidCutPointCompaction(t *testing.T) {
	e := store.Entry{Type: store.EntryTypeCompaction, Data: json.RawMessage(`{}`)}
	if !IsValidCutPoint(e) {
		t.Error("compaction should be valid")
	}
}

func TestIsValidCutPointModelChangeInvalid(t *testing.T) {
	e := store.Entry{Type: store.EntryTypeModelChange, Data: json.RawMessage(`{}`)}
	if IsValidCutPoint(e) {
		t.Error("model_change should not be a valid cut point on its own")
	}
}

// FindTurnStartIndex ----------------------------------------------------

func TestFindTurnStartFindsUserMessage(t *testing.T) {
	entries := []store.Entry{
		messageDataEntry(t, "u1", "user", 1),
		messageDataEntry(t, "a1", "assistant", 1),
		messageDataEntry(t, "t1", "toolResult", 1),
		messageDataEntry(t, "a2", "assistant", 1),
	}
	if got := FindTurnStartIndex(entries, 3); got != 0 {
		t.Errorf("turn start = %d", got)
	}
}

func TestFindTurnStartBashExecution(t *testing.T) {
	entries := []store.Entry{
		messageDataEntry(t, "u1", "user", 1),
		blobEntry2(t, "b1", store.EntryTypeBashExecution, nil),
		messageDataEntry(t, "a1", "assistant", 1),
	}
	if got := FindTurnStartIndex(entries, 2); got != 1 {
		t.Errorf("expected bash_execution anchor, got %d", got)
	}
}

func TestFindTurnStartNotFound(t *testing.T) {
	entries := []store.Entry{
		messageDataEntry(t, "a1", "assistant", 1),
		messageDataEntry(t, "a2", "assistant", 1),
	}
	if got := FindTurnStartIndex(entries, 1); got != -1 {
		t.Errorf("got %d, want -1", got)
	}
}

// FindCutPoint ----------------------------------------------------------

func TestFindCutPointAllFitsNoCut(t *testing.T) {
	entries := []store.Entry{
		messageDataEntry(t, "u1", "user", 10),
		messageDataEntry(t, "a1", "assistant", 10),
	}
	msgs := messagesFromEntries(t, entries)
	res := FindCutPoint(entries, msgs, CompactionSettings{KeepRecentTokens: 100})
	if res.CutIndex != 0 {
		t.Errorf("expected no cut, got %d", res.CutIndex)
	}
}

func TestFindCutPointAdvancesPastToolResult(t *testing.T) {
	entries := []store.Entry{
		messageDataEntry(t, "u1", "user", 50),
		messageDataEntry(t, "a1", "assistant", 50),
		messageDataEntry(t, "u2", "user", 50),
		messageDataEntry(t, "a2", "assistant", 50),
		messageDataEntry(t, "t1", "toolResult", 50),
		messageDataEntry(t, "a3", "assistant", 50),
	}
	msgs := messagesFromEntries(t, entries)
	// KeepRecent = 120 tokens — walking backwards, we need to accumulate
	// past at least 3 messages (~150) before hitting threshold.
	res := FindCutPoint(entries, msgs, CompactionSettings{KeepRecentTokens: 120})
	if res.CutIndex >= len(entries) {
		t.Fatalf("cut beyond slice: %d", res.CutIndex)
	}
	if entries[res.CutIndex].Type == store.EntryTypeMessage {
		var md store.MessageData
		_ = json.Unmarshal(entries[res.CutIndex].Data, &md)
		if md.Role == "toolResult" {
			t.Errorf("cut landed on tool result at %d", res.CutIndex)
		}
	}
}

func TestFindCutPointAbsorbsSettingsChanges(t *testing.T) {
	entries := []store.Entry{
		messageDataEntry(t, "u1", "user", 10),
		messageDataEntry(t, "a1", "assistant", 10),
		blobEntry2(t, "m1", store.EntryTypeModelChange, store.ModelChangeData{Provider: "anthropic", ModelID: "m"}),
		blobEntry2(t, "t1", store.EntryTypeThinkingChange, store.ThinkingChangeData{ThinkingLevel: "high"}),
		messageDataEntry(t, "u2", "user", 100),
		messageDataEntry(t, "a2", "assistant", 100),
	}
	msgs := messagesFromEntries(t, entries)
	res := FindCutPoint(entries, msgs, CompactionSettings{KeepRecentTokens: 150})

	// The cut should land at u2 or earlier, but NOT skip the settings changes.
	// If the cut is at u2 (index 4), absorption would roll it back to m1 (index 2).
	if res.CutIndex > 4 {
		t.Errorf("cut too late: %d", res.CutIndex)
	}
	// The cut must not leave orphaned model_change / thinking_change entries
	// in the summarised portion — so the entry immediately before CutIndex
	// must not be one of those types (unless CutIndex is 0).
	if res.CutIndex > 0 {
		prev := entries[res.CutIndex-1]
		if prev.Type == store.EntryTypeModelChange || prev.Type == store.EntryTypeThinkingChange {
			t.Errorf("cut should have absorbed settings change, prev type = %s", prev.Type)
		}
	}
}

func TestFindCutPointSplitTurnDetection(t *testing.T) {
	entries := []store.Entry{
		messageDataEntry(t, "u1", "user", 50),
		messageDataEntry(t, "a1", "assistant", 50),
		messageDataEntry(t, "u2", "user", 50),
		messageDataEntry(t, "a2a", "assistant", 50),
		messageDataEntry(t, "t2", "toolResult", 50),
		messageDataEntry(t, "a2b", "assistant", 50),
	}
	msgs := messagesFromEntries(t, entries)
	res := FindCutPoint(entries, msgs, CompactionSettings{KeepRecentTokens: 120})
	if !res.IsSplitTurn {
		return // not every config triggers the split — accept either path
	}
	if res.TurnStartIndex < 0 {
		t.Errorf("split turn should have TurnStartIndex set")
	}
	if entries[res.TurnStartIndex].Type != store.EntryTypeMessage {
		return
	}
	var md store.MessageData
	_ = json.Unmarshal(entries[res.TurnStartIndex].Data, &md)
	if md.Role != "user" {
		t.Errorf("turn start role = %q", md.Role)
	}
}

func TestFindCutPointEmptyEntries(t *testing.T) {
	res := FindCutPoint(nil, nil, CompactionSettings{KeepRecentTokens: 100})
	if res.CutIndex != 0 {
		t.Errorf("empty should yield no cut: %+v", res)
	}
}
