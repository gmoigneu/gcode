package compaction

import (
	"encoding/json"

	"github.com/gmoigneu/gcode/pkg/agent"
	"github.com/gmoigneu/gcode/pkg/ai"
)

// Approximate chars-per-token ratio used by the estimators.
const charsPerToken = 4

// ImageTokenChars is the chars-equivalent estimate used for image content
// (matches pi: ~1200 tokens == 4800 characters).
const ImageTokenChars = 4800

// EstimateTokens approximates the token count of a single AgentMessage by
// walking its content and summing per-block character counts.
func EstimateTokens(msg agent.AgentMessage) int {
	if msg == nil {
		return 0
	}
	chars := 0
	switch m := msg.(type) {
	case *ai.UserMessage:
		for _, c := range m.Content {
			chars += contentChars(c)
		}
	case *ai.AssistantMessage:
		for _, c := range m.Content {
			chars += contentChars(c)
		}
	case *ai.ToolResultMessage:
		for _, c := range m.Content {
			chars += contentChars(c)
		}
	}
	return chars / charsPerToken
}

func contentChars(c ai.Content) int {
	switch v := c.(type) {
	case *ai.TextContent:
		return len(v.Text)
	case *ai.ThinkingContent:
		return len(v.Thinking)
	case *ai.ImageContent:
		return ImageTokenChars
	case *ai.ToolCall:
		data, _ := json.Marshal(v.Arguments)
		return len(data)
	}
	return 0
}

// CalculateContextTokens extracts the total context size from an ai.Usage
// struct. Prefers TotalTokens when the provider reports it; otherwise sums
// the individual components.
func CalculateContextTokens(usage ai.Usage) int {
	if usage.TotalTokens > 0 {
		return usage.TotalTokens
	}
	return usage.Input + usage.Output + usage.CacheRead + usage.CacheWrite
}

// EstimateContextTokens estimates the full context token count for a
// message list. It prefers ground-truth usage data when available, falling
// back to the chars/4 heuristic only for messages after the most recent
// assistant message with usage, or for the whole list if none is found.
func EstimateContextTokens(messages []agent.AgentMessage) int {
	if len(messages) == 0 {
		return 0
	}

	lastUsageIdx := -1
	base := 0
	for i := len(messages) - 1; i >= 0; i-- {
		am, ok := messages[i].(*ai.AssistantMessage)
		if !ok {
			continue
		}
		if tokens := CalculateContextTokens(am.Usage); tokens > 0 {
			lastUsageIdx = i
			base = tokens
			break
		}
	}

	if lastUsageIdx == -1 {
		total := 0
		for _, m := range messages {
			total += EstimateTokens(m)
		}
		return total
	}

	trailing := 0
	for i := lastUsageIdx + 1; i < len(messages); i++ {
		trailing += EstimateTokens(messages[i])
	}
	return base + trailing
}
