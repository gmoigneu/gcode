package ai

import (
	"strings"
	"testing"
)

func TestTransformEmpty(t *testing.T) {
	got := TransformMessages(nil, Model{ID: "m"})
	if len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

func TestTransformKeepsUserMessages(t *testing.T) {
	msgs := []Message{
		&UserMessage{Content: []Content{&TextContent{Text: "hi"}}, Timestamp: 1},
	}
	got := TransformMessages(msgs, Model{ID: "m"})
	if len(got) != 1 {
		t.Fatalf("len = %d", len(got))
	}
	if _, ok := got[0].(*UserMessage); !ok {
		t.Errorf("got %T", got[0])
	}
}

func TestTransformSkipsErroredAssistant(t *testing.T) {
	msgs := []Message{
		&UserMessage{Content: []Content{&TextContent{Text: "hi"}}},
		&AssistantMessage{
			Model:      "m",
			Content:    []Content{&TextContent{Text: "err"}},
			StopReason: StopReasonError,
		},
		&UserMessage{Content: []Content{&TextContent{Text: "again"}}},
	}
	got := TransformMessages(msgs, Model{ID: "m"})
	if len(got) != 2 {
		t.Fatalf("len = %d", len(got))
	}
	for _, m := range got {
		if _, ok := m.(*AssistantMessage); ok {
			t.Error("errored assistant should be removed")
		}
	}
}

func TestTransformSkipsAbortedAssistant(t *testing.T) {
	msgs := []Message{
		&AssistantMessage{
			Model:      "m",
			Content:    []Content{&TextContent{Text: "abort"}},
			StopReason: StopReasonAborted,
		},
	}
	got := TransformMessages(msgs, Model{ID: "m"})
	if len(got) != 0 {
		t.Errorf("aborted assistant should be removed, got %v", got)
	}
}

func TestTransformThinkingSameModelKept(t *testing.T) {
	msgs := []Message{
		&AssistantMessage{
			Model: "m",
			Content: []Content{
				&ThinkingContent{Thinking: "plan", ThinkingSignature: "sig"},
				&TextContent{Text: "answer"},
			},
			StopReason: StopReasonStop,
		},
	}
	got := TransformMessages(msgs, Model{ID: "m"})
	a := got[0].(*AssistantMessage)
	if len(a.Content) != 2 {
		t.Fatalf("content len = %d", len(a.Content))
	}
	tc, ok := a.Content[0].(*ThinkingContent)
	if !ok {
		t.Fatalf("got %T", a.Content[0])
	}
	if tc.ThinkingSignature != "sig" {
		t.Errorf("signature lost: %+v", tc)
	}
}

func TestTransformThinkingCrossModelConvertedToText(t *testing.T) {
	msgs := []Message{
		&AssistantMessage{
			Model: "other-model",
			Content: []Content{
				&ThinkingContent{Thinking: "plan", ThinkingSignature: "sig"},
				&TextContent{Text: "answer"},
			},
			StopReason: StopReasonStop,
		},
	}
	got := TransformMessages(msgs, Model{ID: "m"})
	a := got[0].(*AssistantMessage)
	if _, ok := a.Content[0].(*ThinkingContent); ok {
		t.Error("cross-model thinking should not remain as ThinkingContent")
	}
	tc, ok := a.Content[0].(*TextContent)
	if !ok {
		t.Fatalf("content[0] = %T", a.Content[0])
	}
	if !strings.Contains(tc.Text, "plan") {
		t.Errorf("converted text = %q", tc.Text)
	}
}

func TestTransformThinkingRedactedCrossModelDropped(t *testing.T) {
	msgs := []Message{
		&AssistantMessage{
			Model: "other",
			Content: []Content{
				&ThinkingContent{Thinking: "secret", Redacted: true},
				&TextContent{Text: "answer"},
			},
			StopReason: StopReasonStop,
		},
	}
	got := TransformMessages(msgs, Model{ID: "m"})
	a := got[0].(*AssistantMessage)
	for _, c := range a.Content {
		if _, ok := c.(*ThinkingContent); ok {
			t.Error("redacted cross-model thinking should be dropped")
		}
	}
	if len(a.Content) != 1 {
		t.Errorf("len = %d; only text should remain", len(a.Content))
	}
}

func TestTransformEmptyThinkingDropped(t *testing.T) {
	msgs := []Message{
		&AssistantMessage{
			Model: "m",
			Content: []Content{
				&ThinkingContent{Thinking: ""},
				&TextContent{Text: "answer"},
			},
			StopReason: StopReasonStop,
		},
	}
	got := TransformMessages(msgs, Model{ID: "m"})
	a := got[0].(*AssistantMessage)
	if len(a.Content) != 1 {
		t.Errorf("empty thinking should be dropped, content=%v", a.Content)
	}
}

func TestTransformToolCallCrossModelStripsSignature(t *testing.T) {
	msgs := []Message{
		&AssistantMessage{
			Model: "other",
			Content: []Content{
				&ToolCall{ID: "c1", Name: "read", ThoughtSignature: "sig"},
			},
			StopReason: StopReasonToolUse,
		},
		&ToolResultMessage{ToolCallID: "c1", ToolName: "read"},
	}
	got := TransformMessages(msgs, Model{ID: "m"})
	a := got[0].(*AssistantMessage)
	tc := a.Content[0].(*ToolCall)
	if tc.ThoughtSignature != "" {
		t.Errorf("signature should be stripped cross-model, got %q", tc.ThoughtSignature)
	}
}

func TestTransformToolCallSameModelKeepsSignature(t *testing.T) {
	msgs := []Message{
		&AssistantMessage{
			Model: "m",
			Content: []Content{
				&ToolCall{ID: "c1", Name: "read", ThoughtSignature: "sig"},
			},
			StopReason: StopReasonToolUse,
		},
		&ToolResultMessage{ToolCallID: "c1", ToolName: "read"},
	}
	got := TransformMessages(msgs, Model{ID: "m"})
	a := got[0].(*AssistantMessage)
	tc := a.Content[0].(*ToolCall)
	if tc.ThoughtSignature != "sig" {
		t.Errorf("signature should be preserved same-model, got %q", tc.ThoughtSignature)
	}
}

func TestTransformOrphanedToolCallGetsSyntheticResult(t *testing.T) {
	msgs := []Message{
		&AssistantMessage{
			Model: "m",
			Content: []Content{
				&ToolCall{ID: "c1", Name: "read"},
				&ToolCall{ID: "c2", Name: "bash"},
			},
			StopReason: StopReasonToolUse,
		},
		&ToolResultMessage{ToolCallID: "c1", ToolName: "read"},
		// c2 is orphaned
		&UserMessage{Content: []Content{&TextContent{Text: "next"}}},
	}
	got := TransformMessages(msgs, Model{ID: "m"})

	// Expect: assistant, tool_result c1, synthetic c2, user
	if len(got) != 4 {
		t.Fatalf("len = %d, want 4; got=%+v", len(got), messagesKinds(got))
	}
	synthetic, ok := got[2].(*ToolResultMessage)
	if !ok {
		t.Fatalf("got[2] = %T", got[2])
	}
	if synthetic.ToolCallID != "c2" || synthetic.ToolName != "bash" || !synthetic.IsError {
		t.Errorf("synthetic = %+v", synthetic)
	}
}

func TestTransformAllToolCallsMatched(t *testing.T) {
	msgs := []Message{
		&AssistantMessage{
			Model: "m",
			Content: []Content{
				&ToolCall{ID: "c1", Name: "read"},
				&ToolCall{ID: "c2", Name: "bash"},
			},
			StopReason: StopReasonToolUse,
		},
		&ToolResultMessage{ToolCallID: "c1"},
		&ToolResultMessage{ToolCallID: "c2"},
	}
	got := TransformMessages(msgs, Model{ID: "m"})
	if len(got) != 3 {
		t.Errorf("len = %d (no synthetic expected)", len(got))
	}
}

func TestTransformDoesNotMutateInput(t *testing.T) {
	orig := &AssistantMessage{
		Model: "other",
		Content: []Content{
			&ThinkingContent{Thinking: "plan", ThinkingSignature: "sig"},
			&ToolCall{ID: "c1", Name: "read", ThoughtSignature: "sig2"},
		},
		StopReason: StopReasonToolUse,
	}
	msgs := []Message{orig, &ToolResultMessage{ToolCallID: "c1"}}
	_ = TransformMessages(msgs, Model{ID: "m"})

	// Original must still have its thinking + signature intact.
	if _, ok := orig.Content[0].(*ThinkingContent); !ok {
		t.Error("input thinking mutated")
	}
	if tc := orig.Content[1].(*ToolCall); tc.ThoughtSignature != "sig2" {
		t.Errorf("input tool call signature mutated: %q", tc.ThoughtSignature)
	}
}

// ---- helpers ----

func messagesKinds(ms []Message) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.MessageRole()
	}
	return out
}
