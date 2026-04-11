package compaction

// CompactionSettings configures when compaction should fire and how much
// head-room to leave in the context window.
type CompactionSettings struct {
	// Enabled toggles compaction entirely. When false, ShouldCompact always
	// returns false.
	Enabled bool

	// ReserveTokens is the safety margin subtracted from the context window
	// when computing the compaction threshold.
	ReserveTokens int

	// KeepRecentTokens is the target number of tokens to retain after a
	// compaction fires (the "recent" portion that stays in-context).
	KeepRecentTokens int
}

// DefaultCompactionSettings mirrors pi's defaults.
var DefaultCompactionSettings = CompactionSettings{
	Enabled:          true,
	ReserveTokens:    16384,
	KeepRecentTokens: 20000,
}

// CompactionReason is the tag attached to emitted events and entries.
type CompactionReason string

const (
	CompactionReasonThreshold CompactionReason = "threshold"
	CompactionReasonOverflow  CompactionReason = "overflow"
)

// CompactionEvent notifies the agent/TUI about compaction progress. The
// event flow is owned by the agent session layer; this type is defined here
// so every caller can import the same shape.
type CompactionEvent struct {
	Type   string
	Reason CompactionReason
}
