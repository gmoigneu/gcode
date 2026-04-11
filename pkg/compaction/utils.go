package compaction

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gmoigneu/gcode/pkg/agent"
	"github.com/gmoigneu/gcode/pkg/ai"
)

// maxToolResultChars caps tool result output in the serialised
// conversation so the summariser never chokes on a huge blob.
const maxToolResultChars = 2000

// SerializeConversation renders a message list as plain-text labelled
// blocks suitable for feeding to the summarisation LLM.
func SerializeConversation(messages []agent.AgentMessage) string {
	var b strings.Builder
	for _, msg := range messages {
		switch m := msg.(type) {
		case *ai.UserMessage:
			b.WriteString("[User]: ")
			for _, c := range m.Content {
				if tc, ok := c.(*ai.TextContent); ok {
					b.WriteString(tc.Text)
				}
			}
			b.WriteString("\n\n")
		case *ai.AssistantMessage:
			for _, c := range m.Content {
				switch v := c.(type) {
				case *ai.TextContent:
					b.WriteString("[Assistant]: ")
					b.WriteString(v.Text)
					b.WriteString("\n\n")
				case *ai.ThinkingContent:
					if v.Thinking == "" {
						continue
					}
					b.WriteString("[Assistant thinking]: ")
					b.WriteString(v.Thinking)
					b.WriteString("\n\n")
				case *ai.ToolCall:
					args, _ := json.Marshal(v.Arguments)
					b.WriteString("[Assistant tool calls]: ")
					fmt.Fprintf(&b, "%s(%s)", v.Name, string(args))
					b.WriteString("\n\n")
				}
			}
		case *ai.ToolResultMessage:
			b.WriteString("[Tool result]: ")
			text := extractToolResultText(m)
			if len(text) > maxToolResultChars {
				text = text[:maxToolResultChars] + "... (truncated)"
			}
			b.WriteString(text)
			b.WriteString("\n\n")
		}
	}
	return b.String()
}

func extractToolResultText(msg *ai.ToolResultMessage) string {
	var parts []string
	for _, c := range msg.Content {
		if tc, ok := c.(*ai.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}
