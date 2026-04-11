package compaction

import (
	"testing"

	"github.com/gmoigneu/gcode/pkg/agent"
	"github.com/gmoigneu/gcode/pkg/ai"
)

func TestEstimateTokensText(t *testing.T) {
	msg := &ai.UserMessage{Content: []ai.Content{&ai.TextContent{Text: "hello world"}}}
	// 11 chars / 4 = 2
	if got := EstimateTokens(msg); got != 2 {
		t.Errorf("got %d, want 2", got)
	}
}

func TestEstimateTokensImage(t *testing.T) {
	msg := &ai.UserMessage{Content: []ai.Content{&ai.ImageContent{Data: "AAAA"}}}
	// Image = 4800 chars / 4 = 1200
	if got := EstimateTokens(msg); got != 1200 {
		t.Errorf("got %d, want 1200", got)
	}
}

func TestEstimateTokensToolCall(t *testing.T) {
	msg := &ai.AssistantMessage{Content: []ai.Content{
		&ai.ToolCall{ID: "c1", Name: "read", Arguments: map[string]any{"path": "/tmp/x"}},
	}}
	// JSON of arguments: {"path":"/tmp/x"} = 16 chars / 4 = 4
	got := EstimateTokens(msg)
	if got < 3 || got > 5 {
		t.Errorf("unexpected estimate: %d", got)
	}
}

func TestEstimateTokensThinking(t *testing.T) {
	msg := &ai.AssistantMessage{Content: []ai.Content{
		&ai.ThinkingContent{Thinking: "ponder ponder"},
	}}
	if got := EstimateTokens(msg); got != 3 {
		t.Errorf("got %d, want 3", got)
	}
}

func TestEstimateTokensMixedContent(t *testing.T) {
	msg := &ai.AssistantMessage{Content: []ai.Content{
		&ai.ThinkingContent{Thinking: "plan"},
		&ai.TextContent{Text: "answer"},
		&ai.ImageContent{Data: "AAAA"},
	}}
	// 4 + 6 + 4800 = 4810 / 4 = 1202
	if got := EstimateTokens(msg); got != 1202 {
		t.Errorf("got %d, want 1202", got)
	}
}

func TestEstimateTokensToolResult(t *testing.T) {
	msg := &ai.ToolResultMessage{Content: []ai.Content{
		&ai.TextContent{Text: "file contents"},
	}}
	if got := EstimateTokens(msg); got != 3 {
		t.Errorf("got %d, want 3", got)
	}
}

func TestEstimateTokensNil(t *testing.T) {
	if got := EstimateTokens(nil); got != 0 {
		t.Errorf("nil should return 0, got %d", got)
	}
}

func TestCalculateContextTokensTotal(t *testing.T) {
	usage := ai.Usage{TotalTokens: 123, Input: 999}
	if got := CalculateContextTokens(usage); got != 123 {
		t.Errorf("got %d, want 123", got)
	}
}

func TestCalculateContextTokensFallback(t *testing.T) {
	usage := ai.Usage{Input: 100, Output: 50, CacheRead: 20, CacheWrite: 10}
	if got := CalculateContextTokens(usage); got != 180 {
		t.Errorf("got %d, want 180", got)
	}
}

func TestEstimateContextTokensWithUsage(t *testing.T) {
	asst := &ai.AssistantMessage{
		Content:    []ai.Content{&ai.TextContent{Text: "ignored"}},
		StopReason: ai.StopReasonStop,
		Usage:      ai.Usage{TotalTokens: 5000},
	}
	trailing := &ai.UserMessage{
		Content: []ai.Content{&ai.TextContent{Text: "four chars here!!!!"}}, // 19 chars / 4 = 4
	}
	msgs := []agent.AgentMessage{asst, trailing}
	got := EstimateContextTokens(msgs)
	if got < 5004 || got > 5005 {
		t.Errorf("got %d, want ~5004", got)
	}
}

func TestEstimateContextTokensNoUsage(t *testing.T) {
	msgs := []agent.AgentMessage{
		&ai.UserMessage{Content: []ai.Content{&ai.TextContent{Text: "hello world"}}},   // 11/4 = 2
		&ai.AssistantMessage{Content: []ai.Content{&ai.TextContent{Text: "response"}}}, // 8/4 = 2
	}
	if got := EstimateContextTokens(msgs); got != 4 {
		t.Errorf("got %d, want 4", got)
	}
}

func TestEstimateContextTokensEmpty(t *testing.T) {
	if got := EstimateContextTokens(nil); got != 0 {
		t.Errorf("empty should be 0, got %d", got)
	}
}

func TestEstimateContextTokensWalksBackForLastUsage(t *testing.T) {
	// The most recent assistant message has no usage; an earlier one does.
	early := &ai.AssistantMessage{Content: []ai.Content{&ai.TextContent{Text: "a"}}, Usage: ai.Usage{TotalTokens: 1000}}
	later := &ai.AssistantMessage{Content: []ai.Content{&ai.TextContent{Text: "b"}}} // no usage
	after := &ai.UserMessage{Content: []ai.Content{&ai.TextContent{Text: "xxxx"}}}   // 4/4 = 1

	got := EstimateContextTokens([]agent.AgentMessage{early, later, after})
	// Should use 1000 as base + EstimateTokens(later) + EstimateTokens(after) = 1000 + 0 + 1 = 1001
	if got != 1001 {
		t.Errorf("got %d, want 1001", got)
	}
}
