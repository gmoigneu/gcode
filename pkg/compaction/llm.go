package compaction

import (
	"context"
	"fmt"
	"strings"

	"github.com/gmoigneu/gcode/pkg/ai"
)

// StreamFunc matches ai.StreamFunc. It is declared here so callers (and
// tests) can inject a custom provider without importing the ai package
// twice or depending on the package registry.
type StreamFunc = ai.StreamFunc

// callLLM issues a single-turn summarisation request and returns the
// concatenated text response. The stream function is injected so tests
// can use the faux provider without touching global provider state.
func callLLM(
	ctx context.Context,
	stream StreamFunc,
	model ai.Model,
	apiKey string,
	systemPrompt string,
	userMessage string,
	maxTokens int,
) (string, error) {
	if stream == nil {
		// Fall back to the global registry via ai.Stream.
		p, ok := ai.GetProvider(model.Api)
		if !ok || p.Stream == nil {
			return "", fmt.Errorf("compaction: no provider for api %q", model.Api)
		}
		stream = p.Stream
	}

	max := maxTokens
	opts := &ai.StreamOptions{
		Signal:    ctx,
		APIKey:    apiKey,
		MaxTokens: &max,
	}

	llmCtx := ai.Context{
		SystemPrompt: systemPrompt,
		Messages: []ai.Message{
			&ai.UserMessage{
				Content: []ai.Content{&ai.TextContent{Text: userMessage}},
			},
		},
	}

	s := stream(model, llmCtx, opts)

	var result strings.Builder
	for event := range s.C {
		if event.Type == ai.EventTextDelta {
			result.WriteString(event.Delta)
		}
	}

	final := s.Result()
	if final.StopReason == ai.StopReasonError || final.ErrorMessage != "" {
		return "", fmt.Errorf("compaction: llm error: %s", final.ErrorMessage)
	}

	text := result.String()
	if text == "" {
		// Some providers only deliver the full text in the final Content
		// rather than delta events. Scan the final message.
		for _, c := range final.Content {
			if tc, ok := c.(*ai.TextContent); ok {
				text += tc.Text
			}
		}
	}
	return text, nil
}
