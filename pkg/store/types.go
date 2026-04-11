package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
)

// EntryType is the discriminator stored in the entries.type column.
type EntryType string

// Entry types exposed to the rest of gcode.
const (
	EntryTypeMessage        EntryType = "message"
	EntryTypeThinkingChange EntryType = "thinking_level_change"
	EntryTypeModelChange    EntryType = "model_change"
	EntryTypeCompaction     EntryType = "compaction"
	EntryTypeBranchSummary  EntryType = "branch_summary"
	EntryTypeCustom         EntryType = "custom"
	EntryTypeCustomMessage  EntryType = "custom_message"
	EntryTypeLabel          EntryType = "label"
	EntryTypeSessionInfo    EntryType = "session_info"
	EntryTypeBashExecution  EntryType = "bash_execution"
)

// Session is a single conversation thread.
type Session struct {
	ID            string
	Version       int
	Cwd           string
	ParentSession string
	CreatedAt     int64
	Name          string
}

// Entry is a single timeline event in a session.
type Entry struct {
	ID        string
	SessionID string
	ParentID  string
	Type      EntryType
	Timestamp int64
	Data      json.RawMessage
}

// ----- data blob structs -----

// MessageData is the JSON blob for EntryTypeMessage.
type MessageData struct {
	Role    string          `json:"role"`
	Message json.RawMessage `json:"message"`
}

// ThinkingChangeData is the JSON blob for EntryTypeThinkingChange.
type ThinkingChangeData struct {
	ThinkingLevel string `json:"thinkingLevel"`
}

// ModelChangeData is the JSON blob for EntryTypeModelChange.
type ModelChangeData struct {
	Provider string `json:"provider"`
	ModelID  string `json:"modelId"`
}

// CompactionData is the JSON blob for EntryTypeCompaction.
type CompactionData struct {
	Summary          string   `json:"summary"`
	FirstKeptEntryID string   `json:"firstKeptEntryId"`
	TokensBefore     int      `json:"tokensBefore"`
	ReadFiles        []string `json:"readFiles,omitempty"`
	ModifiedFiles    []string `json:"modifiedFiles,omitempty"`
}

// BranchSummaryData is the JSON blob for EntryTypeBranchSummary.
type BranchSummaryData struct {
	Summary       string   `json:"summary"`
	FromID        string   `json:"fromId"`
	ReadFiles     []string `json:"readFiles,omitempty"`
	ModifiedFiles []string `json:"modifiedFiles,omitempty"`
}

// CustomData is the JSON blob for EntryTypeCustom.
type CustomData struct {
	CustomType string          `json:"customType"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

// CustomMessageData is the JSON blob for EntryTypeCustomMessage.
type CustomMessageData struct {
	Content string `json:"content"`
	Display string `json:"display,omitempty"`
}

// LabelData is the JSON blob for EntryTypeLabel.
type LabelData struct {
	TargetID string `json:"targetId"`
	Label    string `json:"label"`
}

// SessionInfoData is the JSON blob for EntryTypeSessionInfo.
type SessionInfoData struct {
	Name string `json:"name"`
}

// ----- ID generation -----

// NewEntryID returns an 8-char hex identifier used for both entries and
// sessions.
func NewEntryID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// NewSessionID is an alias for NewEntryID to make call sites read nicely.
func NewSessionID() string { return NewEntryID() }
