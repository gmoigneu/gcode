package providers

import (
	"github.com/gmoigneu/gcode/pkg/ai"
)

// convertSystemToAnthropic produces the system blocks for a request, optionally
// placing a cache_control marker on the last block.
func convertSystemToAnthropic(systemPrompt string, cacheControl map[string]any) []anthropicSysBlock {
	if systemPrompt == "" {
		return nil
	}
	block := anthropicSysBlock{Type: "text", Text: systemPrompt}
	if cacheControl != nil {
		block.CacheControl = cacheControl
	}
	return []anthropicSysBlock{block}
}

// convertMessagesToAnthropic translates gcode messages into the Anthropic
// wire format. Tool results are packed into a user message with tool_result
// blocks. The last user message has its final content block tagged with
// cache_control when cacheControl is non-nil.
func convertMessagesToAnthropic(messages []ai.Message, cacheControl map[string]any) []anthropicMessage {
	out := make([]anthropicMessage, 0, len(messages))

	i := 0
	for i < len(messages) {
		switch msg := messages[i].(type) {
		case *ai.UserMessage:
			out = append(out, anthropicMessage{
				Role:    "user",
				Content: convertUserContent(msg.Content),
			})
			i++
		case *ai.AssistantMessage:
			out = append(out, anthropicMessage{
				Role:    "assistant",
				Content: convertAssistantContent(msg.Content),
			})
			i++
		case *ai.ToolResultMessage:
			// Collect a contiguous run of tool results into a single user message.
			var blocks []anthropicContent
			for i < len(messages) {
				tr, ok := messages[i].(*ai.ToolResultMessage)
				if !ok {
					break
				}
				blocks = append(blocks, toolResultBlock(tr))
				i++
			}
			out = append(out, anthropicMessage{Role: "user", Content: blocks})
		default:
			i++
		}
	}

	// Apply cache control to the last user message's final content block.
	if cacheControl != nil {
		for k := len(out) - 1; k >= 0; k-- {
			if out[k].Role != "user" || len(out[k].Content) == 0 {
				continue
			}
			last := &out[k].Content[len(out[k].Content)-1]
			last.CacheControl = cacheControl
			break
		}
	}

	return out
}

func convertUserContent(contents []ai.Content) []anthropicContent {
	out := make([]anthropicContent, 0, len(contents))
	for _, c := range contents {
		switch cc := c.(type) {
		case *ai.TextContent:
			out = append(out, anthropicContent{Type: "text", Text: cc.Text})
		case *ai.ImageContent:
			out = append(out, anthropicContent{
				Type: "image",
				Source: &anthropicImage{
					Type:      "base64",
					MediaType: cc.MimeType,
					Data:      cc.Data,
				},
			})
		}
	}
	return out
}

func convertAssistantContent(contents []ai.Content) []anthropicContent {
	out := make([]anthropicContent, 0, len(contents))
	for _, c := range contents {
		switch cc := c.(type) {
		case *ai.TextContent:
			out = append(out, anthropicContent{Type: "text", Text: cc.Text})
		case *ai.ThinkingContent:
			if cc.Thinking == "" {
				continue
			}
			block := anthropicContent{
				Type:     "thinking",
				Thinking: cc.Thinking,
			}
			if cc.ThinkingSignature != "" {
				block.Signature = cc.ThinkingSignature
			}
			out = append(out, block)
		case *ai.ToolCall:
			out = append(out, anthropicContent{
				Type:  "tool_use",
				ID:    cc.ID,
				Name:  cc.Name,
				Input: cc.Arguments,
			})
		}
	}
	return out
}

func toolResultBlock(tr *ai.ToolResultMessage) anthropicContent {
	// Serialise any text blocks into a plain string; ignore images for now.
	var text string
	for _, c := range tr.Content {
		if tc, ok := c.(*ai.TextContent); ok {
			text += tc.Text
		}
	}
	block := anthropicContent{
		Type:      "tool_result",
		ToolUseID: tr.ToolCallID,
		Content:   text,
	}
	if tr.IsError {
		// Anthropic accepts an is_error flag on tool_result, tunnelled via
		// the Content field when set to an object.
		block.Content = []map[string]any{
			{"type": "text", "text": text},
		}
	}
	return block
}

func convertToolsToAnthropic(tools []ai.Tool) []anthropicTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]anthropicTool, 0, len(tools))
	for _, t := range tools {
		out = append(out, anthropicTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.Parameters,
		})
	}
	return out
}
