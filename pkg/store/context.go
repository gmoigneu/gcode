package store

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/gmoigneu/gcode/pkg/agent"
	"github.com/gmoigneu/gcode/pkg/ai"
)

// SessionContext is the materialised state of a session branch, ready to
// feed into the agent loop.
type SessionContext struct {
	Messages      []agent.AgentMessage
	ThinkingLevel ai.ThinkingLevel
	Model         *SessionModel
}

// SessionModel is a lightweight identifier for the provider/model combination
// captured in a ModelChange entry.
type SessionModel struct {
	Provider string
	ModelID  string
}

// BuildContext walks a branch (root → leaf) and produces the conversation
// that should be sent to the LLM.
//
// Rules:
//   - Track the latest ThinkingLevel and Model from settings-change entries.
//   - If a compaction OR branch summary exists on the branch, anchor from
//     its FirstKeptEntryID/FromID (the most recent such anchor wins).
//   - Emit the compaction/summary text as a synthetic user message at the
//     top of the returned Messages slice.
//   - After the anchor, only message entries (EntryTypeMessage) contribute.
//
// Returns an error if a message entry cannot be deserialised.
func BuildContext(entries []Entry) (*SessionContext, error) {
	ctx := &SessionContext{}

	// Track settings as we walk.
	for _, e := range entries {
		switch e.Type {
		case EntryTypeThinkingChange:
			var d ThinkingChangeData
			if err := json.Unmarshal(e.Data, &d); err == nil {
				ctx.ThinkingLevel = ai.ThinkingLevel(d.ThinkingLevel)
			}
		case EntryTypeModelChange:
			var d ModelChangeData
			if err := json.Unmarshal(e.Data, &d); err == nil {
				ctx.Model = &SessionModel{Provider: d.Provider, ModelID: d.ModelID}
			}
		}
	}

	// Find the most recent compaction or branch_summary anchor.
	anchorIdx := -1
	var anchorSummary string
	var anchorTargetID string

	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		switch e.Type {
		case EntryTypeCompaction:
			var d CompactionData
			if err := json.Unmarshal(e.Data, &d); err != nil {
				return nil, fmt.Errorf("store: decode compaction %s: %w", e.ID, err)
			}
			anchorIdx = i
			anchorSummary = d.Summary
			anchorTargetID = d.FirstKeptEntryID
		case EntryTypeBranchSummary:
			var d BranchSummaryData
			if err := json.Unmarshal(e.Data, &d); err != nil {
				return nil, fmt.Errorf("store: decode branch_summary %s: %w", e.ID, err)
			}
			anchorIdx = i
			anchorSummary = d.Summary
			anchorTargetID = d.FromID
		}
		if anchorIdx != -1 {
			break
		}
	}

	// Determine the slice of entries we actually emit.
	startIdx := 0
	if anchorIdx != -1 {
		// Prefer the FirstKeptEntryID target, if it exists in the branch.
		// If not, fall back to the entry immediately after the anchor.
		target := anchorIdx + 1
		if anchorTargetID != "" {
			for i, e := range entries {
				if e.ID == anchorTargetID {
					target = i
					break
				}
			}
		}
		startIdx = target
		ctx.Messages = append(ctx.Messages, &ai.UserMessage{
			Content: []ai.Content{&ai.TextContent{
				Text: "Previous conversation summary:\n" + anchorSummary,
			}},
			Timestamp: entries[anchorIdx].Timestamp,
		})
	}

	for _, e := range entries[startIdx:] {
		if e.Type != EntryTypeMessage {
			continue
		}
		msg, err := DeserializeMessageEntry(e)
		if err != nil {
			return nil, err
		}
		ctx.Messages = append(ctx.Messages, msg)
	}

	return ctx, nil
}

// DeserializeMessageEntry pulls the AgentMessage out of a MessageData blob.
func DeserializeMessageEntry(e Entry) (agent.AgentMessage, error) {
	var md MessageData
	if err := json.Unmarshal(e.Data, &md); err != nil {
		return nil, fmt.Errorf("store: entry %s: decode message wrapper: %w", e.ID, err)
	}
	if len(md.Message) == 0 {
		return nil, fmt.Errorf("store: entry %s: empty message blob", e.ID)
	}
	return DeserializeMessage(md.Message)
}

// DeserializeMessage decodes a raw AgentMessage JSON blob using the role
// discriminator.
func DeserializeMessage(raw json.RawMessage) (agent.AgentMessage, error) {
	var probe struct {
		Role string `json:"role"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, fmt.Errorf("store: decode role: %w", err)
	}
	switch probe.Role {
	case "user":
		var m ai.UserMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, err
		}
		return &m, nil
	case "assistant":
		var m ai.AssistantMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, err
		}
		return &m, nil
	case "toolResult":
		var m ai.ToolResultMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, err
		}
		return &m, nil
	default:
		return nil, fmt.Errorf("store: unknown message role %q", probe.Role)
	}
}

// SerializeMessageEntry encodes an agent message into the MessageData blob
// shape expected by AppendEntry(EntryTypeMessage).
func SerializeMessageEntry(msg agent.AgentMessage) (MessageData, error) {
	raw, err := json.Marshal(msg)
	if err != nil {
		return MessageData{}, fmt.Errorf("store: encode message: %w", err)
	}
	return MessageData{Role: msg.MessageRole(), Message: raw}, nil
}

// ensure time is still referenced even though BuildContext no longer uses
// it directly — keeps future additions convenient.
var _ = time.Now
