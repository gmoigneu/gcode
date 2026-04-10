package providers

import (
	"encoding/json"
	"fmt"

	"github.com/gmoigneu/gcode/pkg/ai"
)

// convertMessagesToOpenAI translates the gcode message model into the
// OpenAI chat/completions wire format.
func convertMessagesToOpenAI(systemPrompt string, messages []ai.Message, compat ai.OpenAICompat) []openAIMessage {
	out := make([]openAIMessage, 0, len(messages)+1)

	if systemPrompt != "" {
		role := "system"
		if compat.SupportsDeveloperRole {
			role = "developer"
		}
		out = append(out, openAIMessage{Role: role, Content: systemPrompt})
	}

	for _, m := range messages {
		switch msg := m.(type) {
		case *ai.UserMessage:
			out = append(out, convertUserMessageOpenAI(msg))
		case *ai.AssistantMessage:
			out = append(out, convertAssistantMessageOpenAI(msg, compat))
		case *ai.ToolResultMessage:
			out = append(out, convertToolResultMessageOpenAI(msg, compat)...)
		}
	}

	return out
}

func convertUserMessageOpenAI(msg *ai.UserMessage) openAIMessage {
	// If every piece is text and there is exactly one, use the simple string form.
	if len(msg.Content) == 1 {
		if tc, ok := msg.Content[0].(*ai.TextContent); ok {
			return openAIMessage{Role: "user", Content: tc.Text}
		}
	}

	parts := make([]openAIPart, 0, len(msg.Content))
	for _, c := range msg.Content {
		switch cc := c.(type) {
		case *ai.TextContent:
			parts = append(parts, openAIPart{Type: "text", Text: cc.Text})
		case *ai.ImageContent:
			dataURL := fmt.Sprintf("data:%s;base64,%s", cc.MimeType, cc.Data)
			parts = append(parts, openAIPart{
				Type:     "image_url",
				ImageURL: &openAIImageURL{URL: dataURL},
			})
		}
	}
	if len(parts) == 0 {
		return openAIMessage{Role: "user", Content: ""}
	}
	return openAIMessage{Role: "user", Content: parts}
}

func convertAssistantMessageOpenAI(msg *ai.AssistantMessage, compat ai.OpenAICompat) openAIMessage {
	var text string
	var thinking string
	var toolCalls []openAIToolCall

	for _, c := range msg.Content {
		switch cc := c.(type) {
		case *ai.TextContent:
			text += cc.Text
		case *ai.ThinkingContent:
			if compat.RequiresThinkingAsText && cc.Thinking != "" {
				thinking += cc.Thinking
			}
		case *ai.ToolCall:
			args, _ := json.Marshal(cc.Arguments)
			toolCalls = append(toolCalls, openAIToolCall{
				ID:   cc.ID,
				Type: "function",
				Function: openAIToolCallFn{
					Name:      cc.Name,
					Arguments: string(args),
				},
			})
		}
	}

	if thinking != "" {
		// Prefix the assistant content with a thinking marker.
		text = "<thinking>" + thinking + "</thinking>" + text
	}

	out := openAIMessage{Role: "assistant"}
	if text != "" {
		out.Content = text
	}
	if len(toolCalls) > 0 {
		out.ToolCalls = toolCalls
	}
	return out
}

// convertToolResultMessageOpenAI returns one or more OpenAI messages for a
// gcode ToolResultMessage. Text blocks collapse to a single tool message;
// any image blocks follow as a separate user message after the tool result,
// matching pi's behaviour.
func convertToolResultMessageOpenAI(msg *ai.ToolResultMessage, compat ai.OpenAICompat) []openAIMessage {
	var text string
	var imageParts []openAIPart
	for _, c := range msg.Content {
		switch cc := c.(type) {
		case *ai.TextContent:
			text += cc.Text
		case *ai.ImageContent:
			dataURL := fmt.Sprintf("data:%s;base64,%s", cc.MimeType, cc.Data)
			imageParts = append(imageParts, openAIPart{
				Type:     "image_url",
				ImageURL: &openAIImageURL{URL: dataURL},
			})
		}
	}

	tool := openAIMessage{
		Role:       "tool",
		ToolCallID: msg.ToolCallID,
		Content:    text,
	}
	if compat.RequiresToolResultName {
		tool.Name = msg.ToolName
	}
	out := []openAIMessage{tool}

	if len(imageParts) > 0 {
		out = append(out, openAIMessage{Role: "user", Content: imageParts})
	}
	return out
}

// convertToolsToOpenAI returns the tools array for the request body.
func convertToolsToOpenAI(tools []ai.Tool, compat ai.OpenAICompat) []openAITool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]openAITool, 0, len(tools))
	for _, t := range tools {
		out = append(out, openAITool{
			Type: "function",
			Function: openAIFnSchema{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
				Strict:      compat.SupportsStrictMode,
			},
		})
	}
	return out
}
