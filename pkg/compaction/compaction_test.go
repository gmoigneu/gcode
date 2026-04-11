package compaction

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gmoigneu/gcode/pkg/agent"
	"github.com/gmoigneu/gcode/pkg/ai"
	"github.com/gmoigneu/gcode/pkg/ai/providers"
	"github.com/gmoigneu/gcode/pkg/store"
)

// ----- helpers -----

func fauxStreamFunc(text string) StreamFunc {
	f := &providers.FauxProvider{Responses: []providers.FauxResponse{{Text: text}}}
	return f.Stream
}

func testModel() ai.Model {
	return ai.Model{ID: "faux-m", Api: "faux", Provider: "anthropic"}
}

func buildEntriesFromMessages(t *testing.T, msgs []agent.AgentMessage) []store.Entry {
	t.Helper()
	out := make([]store.Entry, 0, len(msgs))
	for i, m := range msgs {
		md, _ := store.SerializeMessageEntry(m)
		raw, _ := json.Marshal(md)
		out = append(out, store.Entry{
			ID:        []string{"e1", "e2", "e3", "e4", "e5", "e6", "e7", "e8"}[i%8],
			Type:      store.EntryTypeMessage,
			Timestamp: int64(i + 1),
			Data:      raw,
		})
	}
	return out
}

// ----- SerializeConversation -----

func TestSerializeConversationUser(t *testing.T) {
	out := SerializeConversation([]agent.AgentMessage{
		&ai.UserMessage{Content: []ai.Content{&ai.TextContent{Text: "hello"}}},
	})
	if !strings.Contains(out, "[User]: hello") {
		t.Errorf("got %q", out)
	}
}

func TestSerializeConversationAssistantText(t *testing.T) {
	out := SerializeConversation([]agent.AgentMessage{
		&ai.AssistantMessage{Content: []ai.Content{&ai.TextContent{Text: "hi there"}}},
	})
	if !strings.Contains(out, "[Assistant]: hi there") {
		t.Errorf("got %q", out)
	}
}

func TestSerializeConversationAssistantThinking(t *testing.T) {
	out := SerializeConversation([]agent.AgentMessage{
		&ai.AssistantMessage{Content: []ai.Content{&ai.ThinkingContent{Thinking: "pondering"}}},
	})
	if !strings.Contains(out, "[Assistant thinking]: pondering") {
		t.Errorf("got %q", out)
	}
}

func TestSerializeConversationToolCall(t *testing.T) {
	out := SerializeConversation([]agent.AgentMessage{
		&ai.AssistantMessage{Content: []ai.Content{
			&ai.ToolCall{ID: "c1", Name: "read", Arguments: map[string]any{"path": "/x"}},
		}},
	})
	if !strings.Contains(out, "[Assistant tool calls]: read(") {
		t.Errorf("got %q", out)
	}
	if !strings.Contains(out, `"path":"/x"`) {
		t.Errorf("args missing: %q", out)
	}
}

func TestSerializeConversationToolResultTruncated(t *testing.T) {
	text := strings.Repeat("x", 3000)
	out := SerializeConversation([]agent.AgentMessage{
		&ai.ToolResultMessage{Content: []ai.Content{&ai.TextContent{Text: text}}},
	})
	if !strings.Contains(out, "... (truncated)") {
		t.Error("expected truncated marker")
	}
	if strings.Count(out, "x") > 2100 {
		t.Errorf("text not truncated, len = %d", len(out))
	}
}

// ----- prompt constants -----

func TestPromptConstantsNonEmpty(t *testing.T) {
	if SummarizationSystemPrompt == "" || SummarizationPrompt == "" {
		t.Error("prompt constants empty")
	}
	if UpdateSummarizationPrompt == "" || TurnPrefixPrompt == "" {
		t.Error("prompt constants empty")
	}
	if BranchSummaryPreamble == "" || BranchSummaryPrompt == "" {
		t.Error("branch prompts empty")
	}
}

func TestSummarizationPromptStructure(t *testing.T) {
	sections := []string{"## Goal", "## Constraints", "## Progress", "## Key Decisions", "## Next Steps", "## Critical Context"}
	for _, s := range sections {
		if !strings.Contains(SummarizationPrompt, s) {
			t.Errorf("missing section %q", s)
		}
	}
}

func TestUpdateSummarizationPromptIncludesPlaceholder(t *testing.T) {
	if !strings.Contains(UpdateSummarizationPrompt, "%s") {
		t.Error("update prompt should have a percent-s placeholder for previous summary")
	}
}

// ----- PrepareCompaction -----

func TestPrepareCompactionSkipsLastCompaction(t *testing.T) {
	entries := []store.Entry{
		{ID: "e1", Type: store.EntryTypeCompaction, Data: json.RawMessage(`{}`)},
	}
	prep := PrepareCompaction(entries, nil, CompactionSettings{KeepRecentTokens: 100})
	if prep != nil {
		t.Error("expected nil when last entry is compaction")
	}
}

func TestPrepareCompactionSkipsEmpty(t *testing.T) {
	if prep := PrepareCompaction(nil, nil, CompactionSettings{KeepRecentTokens: 100}); prep != nil {
		t.Error("expected nil for empty entries")
	}
}

func TestPrepareCompactionIdentifiesMessages(t *testing.T) {
	// Build enough messages that FindCutPoint will decide to cut somewhere.
	var msgs []agent.AgentMessage
	for i := 0; i < 5; i++ {
		msgs = append(msgs, &ai.UserMessage{
			Content: []ai.Content{&ai.TextContent{Text: strings.Repeat("x", 200)}},
		})
		msgs = append(msgs, &ai.AssistantMessage{
			Content: []ai.Content{&ai.TextContent{Text: strings.Repeat("y", 200)}},
		})
	}
	entries := buildEntriesFromMessages(t, msgs)

	prep := PrepareCompaction(entries, msgs, CompactionSettings{KeepRecentTokens: 150})
	if prep == nil {
		t.Fatal("expected preparation")
	}
	if len(prep.MessagesToSummarize) == 0 {
		t.Error("expected messages to summarize")
	}
	if prep.FirstKeptEntryID == "" {
		t.Error("FirstKeptEntryID not set")
	}
}

// ----- Compact -----

func TestCompactProducesSummary(t *testing.T) {
	msgs := []agent.AgentMessage{
		&ai.UserMessage{Content: []ai.Content{&ai.TextContent{Text: "do X"}}},
		&ai.AssistantMessage{Content: []ai.Content{&ai.TextContent{Text: "did X"}}},
	}
	prep := &CompactionPreparation{
		MessagesToSummarize: msgs,
		FirstKeptEntryID:    "e42",
		FileOps:             NewFileOperations(),
		TokensBefore:        1234,
	}

	result, err := Compact(
		context.Background(), prep, testModel(), "",
		CompactionSettings{ReserveTokens: 1000},
		fauxStreamFunc("MOCK SUMMARY"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Summary, "MOCK SUMMARY") {
		t.Errorf("summary = %q", result.Summary)
	}
	if result.FirstKeptEntryID != "e42" {
		t.Errorf("FirstKept = %q", result.FirstKeptEntryID)
	}
	if result.TokensBefore != 1234 {
		t.Errorf("tokensBefore = %d", result.TokensBefore)
	}
}

func TestCompactAppendsFileOpsAsXML(t *testing.T) {
	msgs := []agent.AgentMessage{
		&ai.UserMessage{Content: []ai.Content{&ai.TextContent{Text: "do"}}},
	}
	ops := NewFileOperations()
	ops.Read["/read-only.go"] = true
	ops.Edited["/modified.go"] = true

	prep := &CompactionPreparation{
		MessagesToSummarize: msgs,
		FirstKeptEntryID:    "e1",
		FileOps:             ops,
	}

	result, err := Compact(
		context.Background(), prep, testModel(), "",
		CompactionSettings{ReserveTokens: 1000},
		fauxStreamFunc("summary"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Summary, "<read-files>") {
		t.Errorf("read-files missing: %q", result.Summary)
	}
	if !strings.Contains(result.Summary, "<modified-files>") {
		t.Errorf("modified-files missing: %q", result.Summary)
	}
	if len(result.ReadFiles) != 1 || result.ReadFiles[0] != "/read-only.go" {
		t.Errorf("ReadFiles = %v", result.ReadFiles)
	}
	if len(result.ModifiedFiles) != 1 || result.ModifiedFiles[0] != "/modified.go" {
		t.Errorf("ModifiedFiles = %v", result.ModifiedFiles)
	}
}

func TestCompactMergesPreviousFileOps(t *testing.T) {
	msgs := []agent.AgentMessage{
		&ai.UserMessage{Content: []ai.Content{&ai.TextContent{Text: "new"}}},
	}
	prevOps := NewFileOperations()
	prevOps.Read["/old.go"] = true

	prep := &CompactionPreparation{
		MessagesToSummarize: msgs,
		FirstKeptEntryID:    "e1",
		FileOps:             NewFileOperations(),
		PreviousFileOps:     prevOps,
	}

	result, err := Compact(
		context.Background(), prep, testModel(), "",
		CompactionSettings{ReserveTokens: 1000},
		fauxStreamFunc("summary"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Summary, "/old.go") {
		t.Errorf("previous file ops not merged: %q", result.Summary)
	}
}

func TestCompactEmptyPreparationErrors(t *testing.T) {
	_, err := Compact(
		context.Background(),
		&CompactionPreparation{},
		testModel(), "",
		CompactionSettings{ReserveTokens: 1000},
		fauxStreamFunc("ignored"),
	)
	if err == nil {
		t.Error("expected error for empty preparation")
	}
}

func TestCompactSplitTurnGeneratesBothSummaries(t *testing.T) {
	history := []agent.AgentMessage{
		&ai.UserMessage{Content: []ai.Content{&ai.TextContent{Text: "history"}}},
	}
	turnPrefix := []agent.AgentMessage{
		&ai.UserMessage{Content: []ai.Content{&ai.TextContent{Text: "current turn start"}}},
		&ai.AssistantMessage{Content: []ai.Content{&ai.TextContent{Text: "partial response"}}},
	}

	// FauxProvider returns the same text for every call. We can still check
	// that the split turn path combines two summaries via the "Current Turn"
	// delimiter.
	prep := &CompactionPreparation{
		MessagesToSummarize: history,
		TurnPrefixMessages:  turnPrefix,
		FirstKeptEntryID:    "e1",
		FileOps:             NewFileOperations(),
		IsSplitTurn:         true,
	}

	result, err := Compact(
		context.Background(), prep, testModel(), "",
		CompactionSettings{ReserveTokens: 1000},
		fauxStreamFunc("SUMMARY-TEXT"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Summary, "Current Turn (partial)") {
		t.Errorf("split-turn marker missing: %q", result.Summary)
	}
	// SUMMARY-TEXT should appear at least twice (once for history, once for turn prefix).
	if strings.Count(result.Summary, "SUMMARY-TEXT") < 2 {
		t.Errorf("expected two summaries, got %q", result.Summary)
	}
}

// ----- callLLM -----

func TestCallLLMReturnsText(t *testing.T) {
	text, err := callLLM(
		context.Background(),
		fauxStreamFunc("hello from llm"),
		testModel(), "",
		"system", "user",
		1024,
	)
	if err != nil {
		t.Fatal(err)
	}
	if text != "hello from llm" {
		t.Errorf("got %q", text)
	}
}

func TestCallLLMHandlesError(t *testing.T) {
	errStream := func(model ai.Model, ctx ai.Context, opts *ai.StreamOptions) *ai.AssistantMessageEventStream {
		s := ai.NewAssistantMessageEventStream()
		go func() {
			s.Push(ai.AssistantMessageEvent{
				Type: ai.EventError,
				Error: &ai.AssistantMessage{
					StopReason:   ai.StopReasonError,
					ErrorMessage: "boom",
				},
			})
		}()
		return s
	}
	_, err := callLLM(context.Background(), errStream, testModel(), "", "sys", "usr", 100)
	if err == nil {
		t.Error("expected error")
	}
}
