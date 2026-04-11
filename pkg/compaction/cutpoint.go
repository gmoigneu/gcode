package compaction

import (
	"encoding/json"

	"github.com/gmoigneu/gcode/pkg/agent"
	"github.com/gmoigneu/gcode/pkg/ai"
	"github.com/gmoigneu/gcode/pkg/store"
)

// IsValidCutPoint reports whether an entry is a safe place to split the
// conversation. Splitting anywhere else would orphan a tool result (from a
// LLM's perspective) or a mid-turn setting change.
func IsValidCutPoint(entry store.Entry) bool {
	switch entry.Type {
	case store.EntryTypeCustomMessage,
		store.EntryTypeBashExecution,
		store.EntryTypeBranchSummary,
		store.EntryTypeCompaction:
		return true
	case store.EntryTypeMessage:
		var md store.MessageData
		if err := json.Unmarshal(entry.Data, &md); err != nil {
			return false
		}
		return md.Role == "user" || md.Role == "assistant"
	}
	return false
}

// FindTurnStartIndex walks backwards from fromIndex to locate the entry
// that started the current turn — either a user message or a
// bash_execution entry. Returns -1 if no such entry exists before
// fromIndex.
func FindTurnStartIndex(entries []store.Entry, fromIndex int) int {
	if fromIndex >= len(entries) {
		fromIndex = len(entries) - 1
	}
	for i := fromIndex; i >= 0; i-- {
		e := entries[i]
		if e.Type == store.EntryTypeBashExecution {
			return i
		}
		if e.Type == store.EntryTypeMessage {
			var md store.MessageData
			if err := json.Unmarshal(e.Data, &md); err != nil {
				continue
			}
			if md.Role == "user" {
				return i
			}
		}
	}
	return -1
}

// CutPointResult is the output of FindCutPoint.
type CutPointResult struct {
	// CutIndex is the first entry to keep (i.e. the entries before it are
	// candidates for summarisation). A value of 0 means nothing should be
	// compacted — everything is within the recent window.
	CutIndex int

	// TurnStartIndex is the index of the user/bash entry that started the
	// split turn when IsSplitTurn is true. -1 otherwise.
	TurnStartIndex int

	// IsSplitTurn is true when the cut lands on an assistant entry that is
	// part of a multi-message turn. The turn prefix (TurnStartIndex..CutIndex)
	// is summarised separately.
	IsSplitTurn bool
}

// FindCutPoint walks backwards through entries accumulating tokens until
// it exceeds KeepRecentTokens, then snaps to the nearest valid cut point.
// It also absorbs trailing model / thinking-change entries so settings
// changes aren't summarised away.
//
// messages is a parallel list of decoded AgentMessages that matches the
// sequence of EntryTypeMessage entries. If the lengths don't align,
// token estimation falls back to a naive per-entry heuristic.
func FindCutPoint(entries []store.Entry, messages []agent.AgentMessage, settings CompactionSettings) CutPointResult {
	if len(entries) == 0 {
		return CutPointResult{CutIndex: 0, TurnStartIndex: -1}
	}

	// Build an entry-to-message index so we can map a message entry to its
	// decoded AgentMessage for token estimation. Non-message entries are
	// mapped to -1.
	entryMsgIdx := make([]int, len(entries))
	msgCursor := 0
	for i, e := range entries {
		if e.Type != store.EntryTypeMessage {
			entryMsgIdx[i] = -1
			continue
		}
		if msgCursor < len(messages) {
			entryMsgIdx[i] = msgCursor
			msgCursor++
		} else {
			entryMsgIdx[i] = -1
		}
	}

	// Walk backwards accumulating tokens. If we never exceed the target,
	// nothing needs to be compacted.
	accumulated := 0
	target := 0
	reached := false
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Type == store.EntryTypeMessage {
			if mi := entryMsgIdx[i]; mi >= 0 && mi < len(messages) {
				accumulated += EstimateTokens(messages[mi])
			}
		}
		if accumulated >= settings.KeepRecentTokens {
			target = i
			reached = true
			break
		}
	}
	if !reached {
		return CutPointResult{CutIndex: 0, TurnStartIndex: -1}
	}

	// Snap forward to the nearest valid cut point.
	cutIdx := target
	for cutIdx < len(entries) && !IsValidCutPoint(entries[cutIdx]) {
		cutIdx++
	}
	if cutIdx >= len(entries) {
		// No valid cut point after the target; search backwards instead.
		cutIdx = target - 1
		for cutIdx >= 0 && !IsValidCutPoint(entries[cutIdx]) {
			cutIdx--
		}
		if cutIdx < 0 {
			return CutPointResult{CutIndex: 0, TurnStartIndex: -1}
		}
	}

	// Absorb preceding model/thinking-change entries so settings changes
	// are preserved in the kept region.
	for cutIdx > 0 {
		prev := entries[cutIdx-1]
		if prev.Type == store.EntryTypeThinkingChange || prev.Type == store.EntryTypeModelChange {
			cutIdx--
			continue
		}
		break
	}

	res := CutPointResult{CutIndex: cutIdx, TurnStartIndex: -1}

	// Detect a split turn: the cut lands on an assistant message.
	if cutIdx < len(entries) && entries[cutIdx].Type == store.EntryTypeMessage {
		var md store.MessageData
		if err := json.Unmarshal(entries[cutIdx].Data, &md); err == nil && md.Role != "user" {
			res.IsSplitTurn = true
			res.TurnStartIndex = FindTurnStartIndex(entries, cutIdx-1)
		}
	}
	return res
}

// Ensure ai package stays referenced even when callers only use the
// higher-level helpers.
var _ = ai.StopReasonStop
