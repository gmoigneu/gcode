// Package compaction implements pi-style context window management for
// gcode: token estimation, cut-point selection, LLM-driven summary
// generation, branch summarisation, and trigger logic.
package compaction

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/gmoigneu/gcode/pkg/agent"
	"github.com/gmoigneu/gcode/pkg/ai"
	"github.com/gmoigneu/gcode/pkg/store"
)

// CompactionPreparation holds the state needed to run a compaction. It is
// assembled by PrepareCompaction and passed into Compact.
type CompactionPreparation struct {
	// MessagesToSummarize are the messages being compacted into a summary.
	// For iterative compaction, only messages after the previous compaction
	// entry are included here.
	MessagesToSummarize []agent.AgentMessage

	// TurnPrefixMessages are the messages from the split-turn's user message
	// up to (but not including) the cut point. Summarised separately with
	// TurnPrefixPrompt.
	TurnPrefixMessages []agent.AgentMessage

	// FirstKeptEntryID is the entry ID of the first entry to keep (the cut
	// point). Stored in the resulting CompactionData.
	FirstKeptEntryID string

	// TokensBefore is the estimated context size that prompted compaction.
	TokensBefore int

	// FileOps is the merged file operations touched by the summarised
	// messages.
	FileOps *FileOperations

	// PreviousSummary is the text of the prior compaction summary, if one
	// exists on the branch (iterative compaction). Empty otherwise.
	PreviousSummary string

	// PreviousFileOps is the merged file operations from prior compactions.
	PreviousFileOps *FileOperations

	// IsSplitTurn mirrors CutPointResult.IsSplitTurn.
	IsSplitTurn bool
}

// CompactionResult is the output of a successful Compact call. The fields
// correspond one-to-one to the CompactionData blob stored in the entry.
type CompactionResult struct {
	Summary          string
	FirstKeptEntryID string
	TokensBefore     int
	ReadFiles        []string
	ModifiedFiles    []string
}

// PrepareCompaction inspects a session branch and produces the
// CompactionPreparation for a subsequent Compact call. It returns nil when
// compaction should be skipped (e.g. no messages to summarise, or the
// last entry is already a compaction).
//
// entries and messages must be aligned: messages contains the decoded
// AgentMessage for each EntryTypeMessage entry in order.
func PrepareCompaction(entries []store.Entry, messages []agent.AgentMessage, settings CompactionSettings) *CompactionPreparation {
	if len(entries) == 0 {
		return nil
	}

	if last := entries[len(entries)-1]; last.Type == store.EntryTypeCompaction {
		return nil
	}

	cut := FindCutPoint(entries, messages, settings)
	if cut.CutIndex == 0 {
		return nil
	}

	// Iterative compaction: look for a previous compaction entry anywhere
	// before the cut.
	prevSummary := ""
	var prevOps *FileOperations
	prevCompactionIdx := -1
	for i := cut.CutIndex - 1; i >= 0; i-- {
		if entries[i].Type == store.EntryTypeCompaction {
			var d store.CompactionData
			if err := json.Unmarshal(entries[i].Data, &d); err == nil {
				prevSummary = d.Summary
				prevOps = NewFileOperations()
				for _, f := range d.ReadFiles {
					prevOps.Read[f] = true
				}
				for _, f := range d.ModifiedFiles {
					prevOps.Edited[f] = true
				}
				prevCompactionIdx = i
			}
			break
		}
	}

	entryToMsg := buildEntryMessageIndex(entries, messages)

	fromEntry := 0
	if prevCompactionIdx >= 0 {
		fromEntry = prevCompactionIdx + 1
	}
	toSummarize := collectMessagesBetween(messages, entryToMsg, fromEntry, cut.CutIndex)
	if len(toSummarize) == 0 {
		return nil
	}

	var turnPrefix []agent.AgentMessage
	if cut.IsSplitTurn && cut.TurnStartIndex >= 0 {
		turnPrefix = collectMessagesBetween(messages, entryToMsg, cut.TurnStartIndex, cut.CutIndex)
		toSummarize = removeTail(toSummarize, len(turnPrefix))
	}

	prep := &CompactionPreparation{
		MessagesToSummarize: toSummarize,
		TurnPrefixMessages:  turnPrefix,
		FirstKeptEntryID:    entries[cut.CutIndex].ID,
		TokensBefore:        EstimateContextTokens(messages),
		FileOps:             ExtractFileOpsFromMessages(toSummarize),
		PreviousSummary:     prevSummary,
		PreviousFileOps:     prevOps,
		IsSplitTurn:         cut.IsSplitTurn,
	}
	if prep.IsSplitTurn {
		prep.FileOps.Merge(ExtractFileOpsFromMessages(turnPrefix))
	}
	return prep
}

// buildEntryMessageIndex returns a slice the same length as entries where
// each element is the index into messages for that EntryTypeMessage, or
// -1 for non-message entries.
func buildEntryMessageIndex(entries []store.Entry, messages []agent.AgentMessage) []int {
	out := make([]int, len(entries))
	cursor := 0
	for i, e := range entries {
		if e.Type != store.EntryTypeMessage || cursor >= len(messages) {
			out[i] = -1
			continue
		}
		out[i] = cursor
		cursor++
	}
	return out
}

// collectMessagesBetween returns messages corresponding to entries in
// [entryFrom, entryTo). Non-message entries are skipped.
func collectMessagesBetween(messages []agent.AgentMessage, entryToMsg []int, entryFrom, entryTo int) []agent.AgentMessage {
	if entryFrom < 0 {
		entryFrom = 0
	}
	if entryTo > len(entryToMsg) {
		entryTo = len(entryToMsg)
	}
	var out []agent.AgentMessage
	for i := entryFrom; i < entryTo; i++ {
		mi := entryToMsg[i]
		if mi < 0 {
			continue
		}
		out = append(out, messages[mi])
	}
	return out
}

func removeTail[T any](s []T, n int) []T {
	if n <= 0 {
		return s
	}
	if n >= len(s) {
		return nil
	}
	return s[:len(s)-n]
}

// Compact generates a compaction summary via an LLM call and combines the
// result with file operations. If the cut splits a turn, two summary
// requests are issued concurrently (history + turn prefix) and combined.
//
// stream may be nil, in which case the call falls back to the provider
// registered for model.Api.
func Compact(
	ctx context.Context,
	prep *CompactionPreparation,
	model ai.Model,
	apiKey string,
	settings CompactionSettings,
	stream StreamFunc,
) (*CompactionResult, error) {
	if prep == nil || len(prep.MessagesToSummarize)+len(prep.TurnPrefixMessages) == 0 {
		return nil, fmt.Errorf("compaction: empty preparation")
	}

	maxSummaryTokens := int(float64(settings.ReserveTokens) * 0.8)
	if maxSummaryTokens <= 0 {
		maxSummaryTokens = 8192
	}

	var (
		historySummary    string
		turnPrefixSummary string
		historyErr        error
		prefixErr         error
	)

	if prep.IsSplitTurn && len(prep.TurnPrefixMessages) > 0 {
		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			historySummary, historyErr = generateSummary(
				ctx, stream, model, apiKey,
				prep.MessagesToSummarize, prep.PreviousSummary, maxSummaryTokens,
			)
		}()

		turnPrefixMax := int(float64(settings.ReserveTokens) * 0.5)
		if turnPrefixMax <= 0 {
			turnPrefixMax = 4096
		}
		go func() {
			defer wg.Done()
			turnPrefixSummary, prefixErr = generateTurnPrefixSummary(
				ctx, stream, model, apiKey, prep.TurnPrefixMessages, turnPrefixMax,
			)
		}()

		wg.Wait()
		if historyErr != nil {
			return nil, historyErr
		}
		if prefixErr != nil {
			return nil, prefixErr
		}
	} else {
		historySummary, historyErr = generateSummary(
			ctx, stream, model, apiKey,
			prep.MessagesToSummarize, prep.PreviousSummary, maxSummaryTokens,
		)
		if historyErr != nil {
			return nil, historyErr
		}
	}

	final := historySummary
	if turnPrefixSummary != "" {
		final = historySummary + "\n\n---\n\n## Current Turn (partial)\n\n" + turnPrefixSummary
	}

	merged := prep.FileOps
	if merged == nil {
		merged = NewFileOperations()
	}
	if prep.PreviousFileOps != nil {
		merged.Merge(prep.PreviousFileOps)
	}
	readOnly, modified := merged.ComputeFileLists()
	if ops := FormatFileOperations(readOnly, modified); ops != "" {
		final += "\n\n" + ops
	}

	return &CompactionResult{
		Summary:          final,
		FirstKeptEntryID: prep.FirstKeptEntryID,
		TokensBefore:     prep.TokensBefore,
		ReadFiles:        readOnly,
		ModifiedFiles:    modified,
	}, nil
}

func generateSummary(
	ctx context.Context,
	stream StreamFunc,
	model ai.Model,
	apiKey string,
	messages []agent.AgentMessage,
	previousSummary string,
	maxTokens int,
) (string, error) {
	if len(messages) == 0 {
		return "", nil
	}
	conversation := SerializeConversation(messages)

	var userPrompt string
	if previousSummary != "" {
		userPrompt = fmt.Sprintf(UpdateSummarizationPrompt, previousSummary) + "\n\n" + conversation
	} else {
		userPrompt = SummarizationPrompt + "\n\n" + conversation
	}
	return callLLM(ctx, stream, model, apiKey, SummarizationSystemPrompt, userPrompt, maxTokens)
}

func generateTurnPrefixSummary(
	ctx context.Context,
	stream StreamFunc,
	model ai.Model,
	apiKey string,
	messages []agent.AgentMessage,
	maxTokens int,
) (string, error) {
	if len(messages) == 0 {
		return "", nil
	}
	conversation := SerializeConversation(messages)
	userPrompt := TurnPrefixPrompt + "\n\n" + conversation
	return callLLM(ctx, stream, model, apiKey, SummarizationSystemPrompt, userPrompt, maxTokens)
}
