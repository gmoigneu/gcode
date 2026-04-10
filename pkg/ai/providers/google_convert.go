package providers

import (
	"github.com/gmoigneu/gcode/pkg/ai"
)

// convertSystemToGoogle returns the system_instruction field.
func convertSystemToGoogle(systemPrompt string) *googleSystem {
	if systemPrompt == "" {
		return nil
	}
	return &googleSystem{Parts: []googlePart{{Text: systemPrompt}}}
}

// convertMessagesToGoogle maps gcode messages into Gemini's contents array.
// Roles are user/model; tool results are wrapped in a user message with a
// functionResponse part.
func convertMessagesToGoogle(messages []ai.Message) []googleContent {
	out := make([]googleContent, 0, len(messages))

	for _, m := range messages {
		switch msg := m.(type) {
		case *ai.UserMessage:
			parts := userMessageToGoogleParts(msg)
			if len(parts) == 0 {
				continue
			}
			out = append(out, googleContent{Role: "user", Parts: parts})
		case *ai.AssistantMessage:
			parts := assistantMessageToGoogleParts(msg)
			if len(parts) == 0 {
				continue
			}
			out = append(out, googleContent{Role: "model", Parts: parts})
		case *ai.ToolResultMessage:
			part := toolResultToGooglePart(msg)
			out = append(out, googleContent{Role: "user", Parts: []googlePart{part}})
		}
	}
	return out
}

func userMessageToGoogleParts(msg *ai.UserMessage) []googlePart {
	parts := make([]googlePart, 0, len(msg.Content))
	for _, c := range msg.Content {
		switch cc := c.(type) {
		case *ai.TextContent:
			parts = append(parts, googlePart{Text: cc.Text})
		case *ai.ImageContent:
			parts = append(parts, googlePart{
				InlineData: &googleInlineData{
					MimeType: cc.MimeType,
					Data:     cc.Data,
				},
			})
		}
	}
	return parts
}

func assistantMessageToGoogleParts(msg *ai.AssistantMessage) []googlePart {
	parts := make([]googlePart, 0, len(msg.Content))
	for _, c := range msg.Content {
		switch cc := c.(type) {
		case *ai.TextContent:
			parts = append(parts, googlePart{Text: cc.Text})
		case *ai.ThinkingContent:
			// Gemini does not accept thinking blocks on input. Convert to
			// plain text so the model still sees the context.
			if cc.Thinking != "" {
				parts = append(parts, googlePart{Text: cc.Thinking})
			}
		case *ai.ToolCall:
			args := cc.Arguments
			if args == nil {
				args = map[string]any{}
			}
			parts = append(parts, googlePart{
				FunctionCall: &googleFunctionCall{Name: cc.Name, Args: args},
			})
		}
	}
	return parts
}

func toolResultToGooglePart(tr *ai.ToolResultMessage) googlePart {
	// Collapse text into a single "result" field; ignore other content kinds.
	var text string
	for _, c := range tr.Content {
		if tc, ok := c.(*ai.TextContent); ok {
			text += tc.Text
		}
	}
	return googlePart{
		FunctionResponse: &googleFunctionResponse{
			Name:     tr.ToolName,
			Response: map[string]any{"result": text},
		},
	}
}

func convertToolsToGoogle(tools []ai.Tool) []googleToolWrapper {
	if len(tools) == 0 {
		return nil
	}
	decls := make([]googleFunctionDecl, 0, len(tools))
	for _, t := range tools {
		decls = append(decls, googleFunctionDecl{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
		})
	}
	return []googleToolWrapper{{FunctionDeclarations: decls}}
}
