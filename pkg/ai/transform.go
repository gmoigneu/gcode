package ai

import "time"

// TransformMessages normalizes a message list for the given target model.
// It is a pure function: the input slice and the messages it contains are
// not mutated.
//
// Rules:
//  1. AssistantMessages with StopReason error or aborted are dropped.
//  2. Empty ThinkingContent blocks are dropped.
//  3. ThinkingContent blocks are preserved (signature and all) only when
//     the producing model matches the target model; cross-model thinking
//     is converted to plain TextContent unless it was redacted (then
//     dropped).
//  4. ToolCall blocks have their ThoughtSignature stripped cross-model.
//  5. Any orphaned tool call (no matching ToolResultMessage before the
//     next non-tool-result message) gets a synthetic error ToolResult
//     inserted right after the assistant message.
func TransformMessages(messages []Message, targetModel Model) []Message {
	if len(messages) == 0 {
		return nil
	}

	out := make([]Message, 0, len(messages))

	i := 0
	for i < len(messages) {
		m := messages[i]
		asst, ok := m.(*AssistantMessage)
		if !ok {
			out = append(out, m)
			i++
			continue
		}

		if asst.StopReason == StopReasonError || asst.StopReason == StopReasonAborted {
			i++
			continue
		}

		transformed := transformAssistantMessage(asst, targetModel)
		out = append(out, transformed)
		i++ // advance past the assistant message itself

		pending := collectToolCalls(transformed)
		if len(pending) == 0 {
			continue
		}

		// Walk through the tool results that belong to this assistant
		// message (contiguous run), appending them and recording which
		// tool-call IDs they satisfy.
		matched := map[string]bool{}
		for i < len(messages) {
			tr, ok := messages[i].(*ToolResultMessage)
			if !ok {
				break
			}
			matched[tr.ToolCallID] = true
			out = append(out, tr)
			i++
		}

		// Synthetic error results for any tool call that did not get a
		// real response before the next user/assistant turn.
		for _, tc := range pending {
			if matched[tc.ID] {
				continue
			}
			out = append(out, &ToolResultMessage{
				ToolCallID: tc.ID,
				ToolName:   tc.Name,
				Content: []Content{&TextContent{
					Text: "Error: tool call was not executed.",
				}},
				IsError:   true,
				Timestamp: time.Now().UnixMilli(),
			})
		}
	}

	return out
}

// transformAssistantMessage returns a new AssistantMessage with thinking and
// tool-call blocks normalized for the target model. The input message is
// not mutated.
func transformAssistantMessage(msg *AssistantMessage, target Model) *AssistantMessage {
	sameModel := msg.Model == target.ID && msg.Model != ""

	newContent := make([]Content, 0, len(msg.Content))
	for _, c := range msg.Content {
		switch cc := c.(type) {
		case *ThinkingContent:
			if cc.Thinking == "" {
				continue
			}
			if sameModel {
				cpy := *cc
				newContent = append(newContent, &cpy)
				continue
			}
			if cc.Redacted {
				continue
			}
			newContent = append(newContent, &TextContent{Text: cc.Thinking})
		case *ToolCall:
			cpy := *cc
			if !sameModel {
				cpy.ThoughtSignature = ""
			}
			newContent = append(newContent, &cpy)
		case *TextContent:
			cpy := *cc
			newContent = append(newContent, &cpy)
		case *ImageContent:
			cpy := *cc
			newContent = append(newContent, &cpy)
		default:
			newContent = append(newContent, c)
		}
	}

	cpy := *msg
	cpy.Content = newContent
	return &cpy
}

// collectToolCalls returns the tool calls contained in a message, in their
// original order.
func collectToolCalls(msg *AssistantMessage) []*ToolCall {
	var out []*ToolCall
	for _, c := range msg.Content {
		if tc, ok := c.(*ToolCall); ok {
			out = append(out, tc)
		}
	}
	return out
}
