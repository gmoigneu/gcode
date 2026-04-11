package compaction

import (
	"context"
	"fmt"

	"github.com/gmoigneu/gcode/pkg/agent"
	"github.com/gmoigneu/gcode/pkg/ai"
	"github.com/gmoigneu/gcode/pkg/store"
)

// BranchSummaryPreparation holds the input to SummarizeBranch.
type BranchSummaryPreparation struct {
	// Messages are the abandoned-branch messages in chronological order.
	Messages []agent.AgentMessage

	// FileOps is the merged file operations touched on the abandoned branch.
	FileOps *FileOperations

	// FromID is the leaf entry ID of the abandoned branch.
	FromID string
}

// BranchSummaryResult is the output of SummarizeBranch.
type BranchSummaryResult struct {
	Summary       string
	ReadFiles     []string
	ModifiedFiles []string
}

// branchDB is the subset of store.DB that PrepareBranchSummary needs.
// Defined as an interface so tests can stub the database without spinning
// up SQLite.
type branchDB interface {
	GetBranchEntries(fromID, toID string) ([]store.Entry, error)
}

// PrepareBranchSummary collects the messages on the abandoned branch
// segment between oldLeafID and targetID (exclusive on the common
// ancestor). It applies a token budget, walking newest to oldest, so
// very long branches don't overwhelm the summariser.
func PrepareBranchSummary(db branchDB, oldLeafID, targetID string, settings CompactionSettings) (*BranchSummaryPreparation, error) {
	if db == nil {
		return nil, fmt.Errorf("compaction: branch db is nil")
	}
	entries, err := db.GetBranchEntries(oldLeafID, targetID)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}

	// Walk newest → oldest applying a token budget. Only message entries
	// contribute to the budget.
	budget := settings.KeepRecentTokens
	if budget <= 0 {
		budget = DefaultCompactionSettings.KeepRecentTokens
	}

	var collected []store.Entry
	accumulated := 0
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		collected = append([]store.Entry{e}, collected...)
		if e.Type != store.EntryTypeMessage {
			continue
		}
		msg, err := store.DeserializeMessageEntry(e)
		if err != nil {
			continue
		}
		accumulated += EstimateTokens(msg)
		if accumulated >= budget {
			break
		}
	}

	// Deserialise messages in chronological order.
	var messages []agent.AgentMessage
	for _, e := range collected {
		if e.Type != store.EntryTypeMessage {
			continue
		}
		msg, err := store.DeserializeMessageEntry(e)
		if err != nil {
			continue
		}
		messages = append(messages, msg)
	}

	prep := &BranchSummaryPreparation{
		Messages: messages,
		FileOps:  ExtractFileOpsFromMessages(messages),
		FromID:   oldLeafID,
	}
	return prep, nil
}

// SummarizeBranch calls the LLM to produce a summary of an abandoned
// branch and prepends the branch preamble.
func SummarizeBranch(
	ctx context.Context,
	prep *BranchSummaryPreparation,
	model ai.Model,
	apiKey string,
	settings CompactionSettings,
	stream StreamFunc,
) (*BranchSummaryResult, error) {
	if prep == nil || len(prep.Messages) == 0 {
		return nil, fmt.Errorf("compaction: empty branch preparation")
	}

	conversation := SerializeConversation(prep.Messages)
	userPrompt := BranchSummaryPrompt + "\n\n" + conversation

	maxTokens := int(float64(settings.ReserveTokens) * 0.8)
	if maxTokens <= 0 {
		maxTokens = 8192
	}
	body, err := callLLM(ctx, stream, model, apiKey, SummarizationSystemPrompt, userPrompt, maxTokens)
	if err != nil {
		return nil, err
	}

	summary := BranchSummaryPreamble + "\n\n" + body

	ops := prep.FileOps
	if ops == nil {
		ops = NewFileOperations()
	}
	readOnly, modified := ops.ComputeFileLists()
	if text := FormatFileOperations(readOnly, modified); text != "" {
		summary += "\n\n" + text
	}
	return &BranchSummaryResult{
		Summary:       summary,
		ReadFiles:     readOnly,
		ModifiedFiles: modified,
	}, nil
}
