package store

import (
	"encoding/json"
	"testing"

	"github.com/gmoigneu/gcode/pkg/agent"
	"github.com/gmoigneu/gcode/pkg/ai"
)

// helpers --------------------------------------------------------------

func messageEntry(t *testing.T, id string, msg agent.AgentMessage, ts int64) Entry {
	t.Helper()
	md, err := SerializeMessageEntry(msg)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(md)
	return Entry{
		ID:        id,
		Type:      EntryTypeMessage,
		Timestamp: ts,
		Data:      raw,
	}
}

func blobEntry(t *testing.T, id string, typ EntryType, ts int64, payload any) Entry {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return Entry{ID: id, Type: typ, Timestamp: ts, Data: raw}
}

// tests ----------------------------------------------------------------

func TestBuildContextEmpty(t *testing.T) {
	ctx, err := BuildContext(nil)
	if err != nil {
		t.Fatal(err)
	}
	if ctx == nil {
		t.Fatal("ctx nil")
	}
	if len(ctx.Messages) != 0 {
		t.Errorf("messages = %v", ctx.Messages)
	}
}

func TestBuildContextSimpleConversation(t *testing.T) {
	user := &ai.UserMessage{Content: []ai.Content{&ai.TextContent{Text: "hi"}}, Timestamp: 1}
	asst := &ai.AssistantMessage{
		Content:    []ai.Content{&ai.TextContent{Text: "hello"}},
		Api:        ai.ApiAnthropicMessages,
		Provider:   ai.ProviderAnthropic,
		Model:      "claude",
		StopReason: ai.StopReasonStop,
		Timestamp:  2,
	}
	entries := []Entry{
		messageEntry(t, "e1", user, 1),
		messageEntry(t, "e2", asst, 2),
	}

	ctx, err := BuildContext(entries)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx.Messages) != 2 {
		t.Fatalf("messages = %d", len(ctx.Messages))
	}
	if _, ok := ctx.Messages[0].(*ai.UserMessage); !ok {
		t.Errorf("messages[0] = %T", ctx.Messages[0])
	}
	if _, ok := ctx.Messages[1].(*ai.AssistantMessage); !ok {
		t.Errorf("messages[1] = %T", ctx.Messages[1])
	}
}

func TestBuildContextWithCompaction(t *testing.T) {
	u1 := &ai.UserMessage{Content: []ai.Content{&ai.TextContent{Text: "old"}}, Timestamp: 1}
	u2 := &ai.UserMessage{Content: []ai.Content{&ai.TextContent{Text: "keep"}}, Timestamp: 3}

	entries := []Entry{
		messageEntry(t, "e1", u1, 1),
		blobEntry(t, "e2", EntryTypeCompaction, 2, CompactionData{
			Summary:          "short summary",
			FirstKeptEntryID: "e3",
			TokensBefore:     1000,
		}),
		messageEntry(t, "e3", u2, 3),
	}

	ctx, err := BuildContext(entries)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx.Messages) != 2 {
		t.Fatalf("messages = %d", len(ctx.Messages))
	}
	first, ok := ctx.Messages[0].(*ai.UserMessage)
	if !ok {
		t.Fatalf("messages[0] = %T", ctx.Messages[0])
	}
	if tc, ok := first.Content[0].(*ai.TextContent); !ok || tc.Text == "" ||
		tc.Text == "old" {
		t.Errorf("summary not surfaced: %+v", first)
	}
	// The second message must be the "keep" one, not the "old" one.
	second := ctx.Messages[1].(*ai.UserMessage)
	if tc := second.Content[0].(*ai.TextContent); tc.Text != "keep" {
		t.Errorf("unexpected second message: %q", tc.Text)
	}
}

func TestBuildContextWithBranchSummary(t *testing.T) {
	u := &ai.UserMessage{Content: []ai.Content{&ai.TextContent{Text: "old"}}, Timestamp: 1}
	kept := &ai.UserMessage{Content: []ai.Content{&ai.TextContent{Text: "kept"}}, Timestamp: 3}

	entries := []Entry{
		messageEntry(t, "e1", u, 1),
		blobEntry(t, "e2", EntryTypeBranchSummary, 2, BranchSummaryData{
			Summary: "branch summary",
			FromID:  "e3",
		}),
		messageEntry(t, "e3", kept, 3),
	}

	ctx, err := BuildContext(entries)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx.Messages) != 2 {
		t.Fatalf("messages = %d", len(ctx.Messages))
	}
	first := ctx.Messages[0].(*ai.UserMessage)
	if tc := first.Content[0].(*ai.TextContent); tc.Text == "" || tc.Text == "old" {
		t.Errorf("summary missing: %+v", first)
	}
}

func TestBuildContextThinkingChange(t *testing.T) {
	entries := []Entry{
		blobEntry(t, "e1", EntryTypeThinkingChange, 1, ThinkingChangeData{ThinkingLevel: "high"}),
	}
	ctx, err := BuildContext(entries)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.ThinkingLevel != ai.ThinkingHigh {
		t.Errorf("thinking level = %q", ctx.ThinkingLevel)
	}
}

func TestBuildContextLatestSettingsWin(t *testing.T) {
	entries := []Entry{
		blobEntry(t, "e1", EntryTypeThinkingChange, 1, ThinkingChangeData{ThinkingLevel: "low"}),
		blobEntry(t, "e2", EntryTypeModelChange, 2, ModelChangeData{Provider: "anthropic", ModelID: "claude"}),
		blobEntry(t, "e3", EntryTypeThinkingChange, 3, ThinkingChangeData{ThinkingLevel: "high"}),
		blobEntry(t, "e4", EntryTypeModelChange, 4, ModelChangeData{Provider: "openai", ModelID: "gpt"}),
	}
	ctx, err := BuildContext(entries)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.ThinkingLevel != ai.ThinkingHigh {
		t.Errorf("thinking level = %q", ctx.ThinkingLevel)
	}
	if ctx.Model == nil || ctx.Model.ModelID != "gpt" {
		t.Errorf("model = %+v", ctx.Model)
	}
}

func TestBuildContextBranchWithoutCompactionOnOtherBranch(t *testing.T) {
	// Simulates the case where the caller already passed the correct
	// branch (compaction not on this branch).
	u := &ai.UserMessage{Content: []ai.Content{&ai.TextContent{Text: "hello"}}, Timestamp: 1}
	entries := []Entry{messageEntry(t, "e1", u, 1)}
	ctx, err := BuildContext(entries)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx.Messages) != 1 {
		t.Errorf("messages = %d", len(ctx.Messages))
	}
}

func TestSerializeDeserializeRoundTrip(t *testing.T) {
	original := &ai.UserMessage{
		Content:   []ai.Content{&ai.TextContent{Text: "hello"}, &ai.ImageContent{Data: "x", MimeType: "image/png"}},
		Timestamp: 42,
	}
	md, err := SerializeMessageEntry(original)
	if err != nil {
		t.Fatal(err)
	}
	if md.Role != "user" {
		t.Errorf("role = %q", md.Role)
	}

	got, err := DeserializeMessage(md.Message)
	if err != nil {
		t.Fatal(err)
	}
	user, ok := got.(*ai.UserMessage)
	if !ok {
		t.Fatalf("got %T", got)
	}
	if len(user.Content) != 2 || user.Timestamp != 42 {
		t.Errorf("round-trip lost data: %+v", user)
	}
}

func TestDeserializeMessageEntryFromStoredBlob(t *testing.T) {
	u := &ai.UserMessage{Content: []ai.Content{&ai.TextContent{Text: "hi"}}, Timestamp: 1}
	e := messageEntry(t, "e1", u, 1)

	msg, err := DeserializeMessageEntry(e)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := msg.(*ai.UserMessage); !ok {
		t.Errorf("got %T", msg)
	}
}

func TestDeserializeMessageUnknownRole(t *testing.T) {
	_, err := DeserializeMessage(json.RawMessage(`{"role":"system"}`))
	if err == nil {
		t.Error("expected error for unknown role")
	}
}
