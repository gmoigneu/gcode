# Phase 4: Session persistence (`pkg/store`)

> **Project:** gcode. Go 1.24+. Pure Go SQLite via `modernc.org/sqlite`. No CGo.

SQLite replaces JSONL for session persistence. One `~/.gcode/gcode.db` for global data, one `.gcode/project.db` per project. WAL mode for concurrent reads during compaction.

Depends on `pkg/agent` for message types.

## 4.1 Database layer (`pkg/store/db.go`)

```go
type DB struct {
    db *sql.DB
}

// Open opens or creates a SQLite database at the given path.
// Enables WAL mode, foreign keys, and runs pending migrations.
func Open(path string) (*DB, error)

// Close closes the database connection.
func (d *DB) Close() error
```

### Connection setup

```go
func Open(path string) (*DB, error) {
    db, err := sql.Open("sqlite", path)
    if err != nil {
        return nil, err
    }

    // WAL mode: concurrent reads during compaction writes
    if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
        db.Close()
        return nil, err
    }
    if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
        db.Close()
        return nil, err
    }

    store := &DB{db: db}
    if err := store.migrate(); err != nil {
        db.Close()
        return nil, err
    }
    return store, nil
}
```

### Schema (`pkg/store/migrations.go`)

Migrations are numbered functions. A `schema_version` table tracks the current version.

```sql
-- Migration 1: initial schema
CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER NOT NULL
);
INSERT INTO schema_version (version) VALUES (0);

CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    version INTEGER NOT NULL DEFAULT 3,
    cwd TEXT NOT NULL,
    parent_session TEXT,
    created_at INTEGER NOT NULL,
    name TEXT
);

CREATE TABLE entries (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id),
    parent_id TEXT,
    type TEXT NOT NULL,
    timestamp INTEGER NOT NULL,
    data BLOB NOT NULL,
    FOREIGN KEY (parent_id) REFERENCES entries(id)
);

CREATE INDEX idx_entries_session ON entries(session_id);
CREATE INDEX idx_entries_parent ON entries(parent_id);
CREATE INDEX idx_entries_type ON entries(session_id, type);
```

```go
type migration struct {
    version int
    sql     string
}

var migrations = []migration{
    {1, migrationV1SQL},
}

func (d *DB) migrate() error {
    // Create schema_version if not exists
    // Read current version
    // Apply migrations with version > current in a transaction
    // Update schema_version
}
```

### Entry types

```go
type EntryType string

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
```

### Entry data structures

The `data` column stores a JSON blob. Each entry type has its own Go struct for the blob contents:

```go
// MessageData: data blob for EntryTypeMessage
type MessageData struct {
    Role    string          `json:"role"`
    Message json.RawMessage `json:"message"` // full AgentMessage JSON
}

// ThinkingChangeData: data blob for EntryTypeThinkingChange
type ThinkingChangeData struct {
    ThinkingLevel string `json:"thinkingLevel"`
}

// ModelChangeData: data blob for EntryTypeModelChange
type ModelChangeData struct {
    Provider string `json:"provider"`
    ModelID  string `json:"modelId"`
}

// CompactionData: data blob for EntryTypeCompaction
type CompactionData struct {
    Summary          string   `json:"summary"`
    FirstKeptEntryID string   `json:"firstKeptEntryId"`
    TokensBefore     int      `json:"tokensBefore"`
    ReadFiles        []string `json:"readFiles,omitempty"`
    ModifiedFiles    []string `json:"modifiedFiles,omitempty"`
}

// BranchSummaryData: data blob for EntryTypeBranchSummary
type BranchSummaryData struct {
    Summary       string   `json:"summary"`
    FromID        string   `json:"fromId"`
    ReadFiles     []string `json:"readFiles,omitempty"`
    ModifiedFiles []string `json:"modifiedFiles,omitempty"`
}

// CustomData: data blob for EntryTypeCustom
type CustomData struct {
    CustomType string          `json:"customType"`
    Payload    json.RawMessage `json:"payload,omitempty"`
}

// CustomMessageData: data blob for EntryTypeCustomMessage
type CustomMessageData struct {
    Content string `json:"content"`
    Display string `json:"display,omitempty"`
}

// LabelData: data blob for EntryTypeLabel
type LabelData struct {
    TargetID string `json:"targetId"`
    Label    string `json:"label"`
}

// SessionInfoData: data blob for EntryTypeSessionInfo
type SessionInfoData struct {
    Name string `json:"name"`
}
```

### Entry ID generation

```go
func NewEntryID() string {
    b := make([]byte, 4)
    rand.Read(b)
    return hex.EncodeToString(b)
}
```

## 4.2 Session CRUD (`pkg/store/session.go`)

```go
type Session struct {
    ID            string
    Version       int
    Cwd           string
    ParentSession string
    CreatedAt     int64
    Name          string
}

type Entry struct {
    ID        string
    SessionID string
    ParentID  string
    Type      EntryType
    Timestamp int64
    Data      json.RawMessage // raw JSON blob
}

// CreateSession inserts a new session row. Returns the session.
func (d *DB) CreateSession(cwd string) (*Session, error)

// GetSession retrieves a session by ID.
func (d *DB) GetSession(id string) (*Session, error)

// ListSessions returns sessions ordered by created_at descending.
// Accepts optional filters (cwd, limit).
func (d *DB) ListSessions(opts ListSessionsOpts) ([]Session, error)

type ListSessionsOpts struct {
    Cwd   string
    Limit int
}

// UpdateSessionName sets the session name.
func (d *DB) UpdateSessionName(sessionID, name string) error

// DeleteSession removes a session and all its entries.
func (d *DB) DeleteSession(sessionID string) error
```

## 4.3 Entry CRUD (`pkg/store/session.go`)

```go
// AppendEntry inserts an entry. Sets parentID from the current leaf of the session.
// Returns the inserted entry with generated ID and timestamp.
func (d *DB) AppendEntry(sessionID string, parentID string, entryType EntryType, data any) (*Entry, error)

// GetEntry retrieves a single entry by ID.
func (d *DB) GetEntry(id string) (*Entry, error)

// GetEntries returns all entries for a session, ordered by timestamp.
func (d *DB) GetEntries(sessionID string) ([]Entry, error)

// GetChildren returns entries whose parent_id matches the given entry.
func (d *DB) GetChildren(entryID string) ([]Entry, error)
```

### Branch queries with recursive CTEs

```go
// GetBranch returns the entry chain from root to the given entry ID.
// Uses a recursive CTE walking parent_id from the target up to root, then reverses.
func (d *DB) GetBranch(entryID string) ([]Entry, error)
```

SQL:

```sql
WITH RECURSIVE branch(id, parent_id, type, timestamp, data, depth) AS (
    SELECT id, parent_id, type, timestamp, data, 0
    FROM entries WHERE id = ?
    UNION ALL
    SELECT e.id, e.parent_id, e.type, e.timestamp, e.data, b.depth + 1
    FROM entries e
    JOIN branch b ON e.id = b.parent_id
)
SELECT id, parent_id, type, timestamp, data
FROM branch
ORDER BY depth DESC;
```

```go
// GetLeaves returns entries in a session that have no children.
func (d *DB) GetLeaves(sessionID string) ([]Entry, error)
```

SQL:

```sql
SELECT e.id, e.parent_id, e.type, e.timestamp, e.data
FROM entries e
WHERE e.session_id = ?
AND NOT EXISTS (
    SELECT 1 FROM entries c WHERE c.parent_id = e.id
)
ORDER BY e.timestamp DESC;
```

```go
// GetBranchEntries returns entries on a branch between two points.
// Walks from fromID back to common ancestor with toID, returns the divergent segment.
// Used for branch summarization.
func (d *DB) GetBranchEntries(fromID, toID string) ([]Entry, error)
```

This uses two recursive CTEs to find the ancestors of each entry, computes the common ancestor, then returns entries from the common ancestor to `fromID`.

## 4.4 Build context from session tree (`pkg/store/context.go`)

```go
type SessionContext struct {
    Messages      []agent.AgentMessage
    ThinkingLevel ai.ThinkingLevel
    Model         *SessionModel
}

type SessionModel struct {
    Provider string
    ModelID  string
}

// BuildContext constructs the LLM conversation context from a branch of entries.
// Handles compaction boundaries: if a compaction entry exists on the branch,
// only messages after the compaction's FirstKeptEntryID are included,
// prefixed by the compaction summary as a synthetic user message.
func BuildContext(entries []Entry) (*SessionContext, error)
```

### Algorithm

1. Walk entries root to leaf.
2. Track latest `ThinkingLevel` and `Model` from settings-change entries.
3. Find the most recent compaction or branch summary entry on the path.
4. If compaction exists:
   - Emit the compaction summary as a synthetic user message (first message in context).
   - Skip all entries before `FirstKeptEntryID`.
   - Emit entries from `FirstKeptEntryID` onward.
5. If no compaction: emit all message entries.
6. For each message entry: deserialize `json.RawMessage` into the concrete `AgentMessage` type using the `role` field as discriminator.
7. Return `SessionContext`.

### Message deserialization

```go
func DeserializeMessage(raw json.RawMessage) (agent.AgentMessage, error) {
    var probe struct {
        Role string `json:"role"`
    }
    if err := json.Unmarshal(raw, &probe); err != nil {
        return nil, err
    }
    switch probe.Role {
    case "user":
        var msg ai.UserMessage
        return &msg, json.Unmarshal(raw, &msg)
    case "assistant":
        var msg ai.AssistantMessage
        return &msg, json.Unmarshal(raw, &msg)
    case "toolResult":
        var msg ai.ToolResultMessage
        return &msg, json.Unmarshal(raw, &msg)
    default:
        return nil, fmt.Errorf("unknown message role: %s", probe.Role)
    }
}
```

## 4.5 Tests

### Database tests

- Open creates file and runs migrations
- Open on existing DB skips already-applied migrations
- Schema version tracks correctly across multiple migrations
- WAL mode and foreign keys enabled after Open
- In-memory DB (`:memory:`) works for unit tests

### Session CRUD tests

- CreateSession: verify row inserted with generated ID and timestamp
- GetSession: retrieve existing, error on missing
- ListSessions: ordering, cwd filter, limit
- UpdateSessionName: verify name updates
- DeleteSession: cascades to entries

### Entry CRUD tests

- AppendEntry: verify ID generated, timestamp set, data serialized
- GetEntry: retrieve existing, error on missing
- GetEntries: returns all entries for session in timestamp order
- GetChildren: returns direct children of an entry

### Branch query tests

- GetBranch: linear chain returns all entries root to leaf
- GetBranch: branched tree returns correct path
- GetLeaves: identifies leaf nodes (entries with no children)
- GetBranchEntries: returns entries between two points on divergent branches

### Context builder tests

- Simple conversation: user, assistant, user, assistant
- With compaction: summary emitted first, entries before compaction skipped
- With branch summary: treated same as compaction (summary prefixed)
- Model/thinking changes: latest settings returned
- Empty session: no messages returned
- Branch with compaction: compaction on one branch doesn't affect other
- Message deserialization: round-trip marshal/unmarshal preserves data

### Verification criteria

- [ ] `go test ./pkg/store/...` passes
- [ ] SQLite WAL mode enabled (verified in test)
- [ ] Recursive CTE branch queries return correct paths
- [ ] BuildContext handles compaction boundaries correctly
- [ ] Concurrent reads don't block (WAL mode)
- [ ] DeleteSession cascades to entries
- [ ] Message deserialization round-trips correctly


# Phase 5: Context window management (`pkg/compaction`)

Depends on `pkg/ai` (LLM calls), `pkg/store` (entry types, session queries), and `pkg/agent` (message types).

Faithfully replicates pi's compaction logic: threshold-based and overflow-based triggers, split turn handling, iterative compaction with summary merging, file operations tracking, and branch summarization.

## 5.1 Compaction settings

```go
type CompactionSettings struct {
    Enabled          bool
    ReserveTokens    int // default: 16384
    KeepRecentTokens int // default: 20000
}

var DefaultCompactionSettings = CompactionSettings{
    Enabled:          true,
    ReserveTokens:    16384,
    KeepRecentTokens: 20000,
}
```

## 5.2 Token estimation (`pkg/compaction/tokens.go`)

Three estimation functions, each for a different situation.

### Message-level estimation

```go
// EstimateTokens returns an estimated token count for a single message.
// Text content: len(text) / 4
// Images: 1200 tokens (4800 chars equivalent)
// Tool calls: len(json(arguments)) / 4
// Thinking content: len(thinking) / 4
func EstimateTokens(msg agent.AgentMessage) int
```

Implementation:

```go
func EstimateTokens(msg agent.AgentMessage) int {
    chars := 0
    switch m := msg.(type) {
    case *ai.UserMessage:
        for _, c := range m.Content {
            chars += contentChars(c)
        }
    case *ai.AssistantMessage:
        for _, c := range m.Content {
            chars += contentChars(c)
        }
    case *ai.ToolResultMessage:
        for _, c := range m.Content {
            chars += contentChars(c)
        }
    }
    return chars / 4
}

func contentChars(c ai.Content) int {
    switch v := c.(type) {
    case *ai.TextContent:
        return len(v.Text)
    case *ai.ThinkingContent:
        return len(v.Thinking)
    case *ai.ImageContent:
        return 4800 // fixed estimate: 1200 tokens
    case *ai.ToolCall:
        data, _ := json.Marshal(v.Arguments)
        return len(string(data))
    default:
        return 0
    }
}
```

### Usage-based context estimation

```go
// CalculateContextTokens extracts the total context token count from a Usage struct.
// Uses TotalTokens if available, otherwise computes Input + Output + CacheRead + CacheWrite.
func CalculateContextTokens(usage ai.Usage) int {
    if usage.TotalTokens > 0 {
        return usage.TotalTokens
    }
    return usage.Input + usage.Output + usage.CacheRead + usage.CacheWrite
}
```

### Hybrid context estimation (primary entry point)

```go
// EstimateContextTokens estimates the total context size from a message list.
// Strategy:
// 1. Find the last assistant message with valid usage data (TotalTokens > 0 or Input > 0).
// 2. Use its TotalTokens as the base count.
// 3. Estimate trailing messages after that assistant message using chars/4 heuristic.
// 4. If no usage data found, estimate all messages with chars/4.
func EstimateContextTokens(messages []agent.AgentMessage) int
```

Implementation:

```go
func EstimateContextTokens(messages []agent.AgentMessage) int {
    // Walk backwards to find last assistant message with valid usage
    lastUsageIdx := -1
    var baseTokens int
    for i := len(messages) - 1; i >= 0; i-- {
        if am, ok := messages[i].(*ai.AssistantMessage); ok {
            tokens := CalculateContextTokens(am.Usage)
            if tokens > 0 {
                lastUsageIdx = i
                baseTokens = tokens
                break
            }
        }
    }

    if lastUsageIdx == -1 {
        // No usage data: estimate everything
        total := 0
        for _, m := range messages {
            total += EstimateTokens(m)
        }
        return total
    }

    // Base from usage + estimate trailing messages
    trailing := 0
    for i := lastUsageIdx + 1; i < len(messages); i++ {
        trailing += EstimateTokens(messages[i])
    }
    return baseTokens + trailing
}
```

## 5.3 Compaction trigger (`pkg/compaction/trigger.go`)

### Threshold check

```go
// ShouldCompact returns true when context tokens exceed the safe threshold.
// Threshold = contextWindow - reserveTokens.
func ShouldCompact(contextTokens, contextWindow int, settings CompactionSettings) bool {
    if !settings.Enabled {
        return false
    }
    return contextTokens > contextWindow-settings.ReserveTokens
}
```

### Overflow detection

```go
// OverflowPatterns are provider-specific error message patterns that indicate
// the context window was exceeded. Compiled once at init.
var OverflowPatterns = []*regexp.Regexp{
    // Anthropic
    regexp.MustCompile(`prompt is too long`),
    regexp.MustCompile(`maximum context length`),
    regexp.MustCompile(`exceeds the maximum`),
    regexp.MustCompile(`too many tokens`),
    regexp.MustCompile(`context_length_exceeded`),
    regexp.MustCompile(`content_too_large`),

    // OpenAI
    regexp.MustCompile(`maximum context length`),
    regexp.MustCompile(`This model's maximum context length is`),
    regexp.MustCompile(`context_length_exceeded`),
    regexp.MustCompile(`max_tokens`),
    regexp.MustCompile(`please reduce`),

    // Google
    regexp.MustCompile(`exceeds the maximum number of tokens`),
    regexp.MustCompile(`RESOURCE_EXHAUSTED`),
    regexp.MustCompile(`too long`),

    // Generic
    regexp.MustCompile(`token limit`),
    regexp.MustCompile(`context window`),
    regexp.MustCompile(`input too long`),
    regexp.MustCompile(`request too large`),
}

// NonOverflowPatterns excludes rate limiting and throttling errors
// that match some overflow patterns but are not context overflows.
var NonOverflowPatterns = []*regexp.Regexp{
    regexp.MustCompile(`rate limit`),
    regexp.MustCompile(`rate_limit`),
    regexp.MustCompile(`throttl`),
    regexp.MustCompile(`too many requests`),
    regexp.MustCompile(`429`),
    regexp.MustCompile(`quota`),
    regexp.MustCompile(`billing`),
}

// IsContextOverflow checks if an error message indicates a context window overflow.
// Also handles silent overflow: when usage.Input > contextWindow.
func IsContextOverflow(errMsg string, usage *ai.Usage, contextWindow int) bool {
    // Check silent overflow first
    if usage != nil && contextWindow > 0 && usage.Input > contextWindow {
        return true
    }

    if errMsg == "" {
        return false
    }

    lower := strings.ToLower(errMsg)

    // Exclude non-overflow patterns first
    for _, p := range NonOverflowPatterns {
        if p.MatchString(lower) {
            return false
        }
    }

    // Check overflow patterns
    for _, p := range OverflowPatterns {
        if p.MatchString(lower) {
            return true
        }
    }

    return false
}
```

### Auto-compaction flow

This logic lives in the agent session layer (will be wired in Phase 7), but the interface is defined here:

```go
// CompactionReason indicates why compaction was triggered.
type CompactionReason string

const (
    CompactionReasonThreshold CompactionReason = "threshold"
    CompactionReasonOverflow  CompactionReason = "overflow"
)

// CompactionEvent is emitted to notify the agent/TUI about compaction state.
type CompactionEvent struct {
    Type   string           // "compaction_start" or "compaction_end"
    Reason CompactionReason
}
```

The agent session layer implements this flow:

1. After each agent turn completes (`agent_end` event), check if the last assistant message exists.
2. **Overflow case:** If the assistant message has `StopReason == "error"` and `IsContextOverflow(errorMessage, usage, contextWindow)` is true:
   - Remove the error message from state.
   - Emit `compaction_start` event with reason `"overflow"`.
   - Run `Compact()`.
   - Persist the compaction entry.
   - Reload session context into agent state.
   - Emit `compaction_end` event.
   - Retry the turn with `agent.Continue()`.
   - Only attempt overflow recovery once per turn (guard with `overflowRecoveryAttempted` flag).
3. **Threshold case:** If `ShouldCompact(EstimateContextTokens(messages), contextWindow, settings)`:
   - Emit `compaction_start` event with reason `"threshold"`.
   - Run `Compact()`.
   - Persist the compaction entry.
   - Reload session context.
   - Emit `compaction_end` event.

## 5.4 File operations tracking (`pkg/compaction/fileops.go`)

```go
// FileOperations tracks which files were read, written, or edited during a conversation.
type FileOperations struct {
    Read    map[string]bool // files opened with read tool
    Written map[string]bool // files created/overwritten with write tool
    Edited  map[string]bool // files modified with edit tool
}

func NewFileOperations() *FileOperations {
    return &FileOperations{
        Read:    make(map[string]bool),
        Written: make(map[string]bool),
        Edited:  make(map[string]bool),
    }
}

// ExtractFileOpsFromMessage extracts file paths from tool calls in a message.
// Recognizes read, write, edit tool names and extracts the "path" argument.
func ExtractFileOpsFromMessage(msg agent.AgentMessage) *FileOperations {
    ops := NewFileOperations()
    am, ok := msg.(*ai.AssistantMessage)
    if !ok {
        return ops
    }
    for _, c := range am.Content {
        tc, ok := c.(*ai.ToolCall)
        if !ok {
            continue
        }
        path, _ := tc.Arguments["path"].(string)
        if path == "" {
            continue
        }
        switch tc.Name {
        case "read":
            ops.Read[path] = true
        case "write":
            ops.Written[path] = true
        case "edit":
            ops.Edited[path] = true
        }
    }
    return ops
}

// ExtractFileOpsFromMessages accumulates file operations from all messages.
func ExtractFileOpsFromMessages(messages []agent.AgentMessage) *FileOperations {
    ops := NewFileOperations()
    for _, msg := range messages {
        msgOps := ExtractFileOpsFromMessage(msg)
        ops.Merge(msgOps)
    }
    return ops
}

// Merge combines another FileOperations into this one.
func (f *FileOperations) Merge(other *FileOperations) {
    for k := range other.Read {
        f.Read[k] = true
    }
    for k := range other.Written {
        f.Written[k] = true
    }
    for k := range other.Edited {
        f.Edited[k] = true
    }
}

// ComputeFileLists separates files into read-only (read but never modified)
// and modified (written or edited) sets.
func (f *FileOperations) ComputeFileLists() (readOnly []string, modified []string) {
    modSet := make(map[string]bool)
    for k := range f.Written {
        modSet[k] = true
    }
    for k := range f.Edited {
        modSet[k] = true
    }

    for k := range f.Read {
        if !modSet[k] {
            readOnly = append(readOnly, k)
        }
    }
    for k := range modSet {
        modified = append(modified, k)
    }

    sort.Strings(readOnly)
    sort.Strings(modified)
    return
}

// FormatFileOperations renders file lists as XML tags for inclusion in summaries.
func FormatFileOperations(readOnly, modified []string) string {
    var sb strings.Builder
    if len(readOnly) > 0 {
        sb.WriteString("<read-files>\n")
        for _, f := range readOnly {
            sb.WriteString(f + "\n")
        }
        sb.WriteString("</read-files>\n")
    }
    if len(modified) > 0 {
        sb.WriteString("<modified-files>\n")
        for _, f := range modified {
            sb.WriteString(f + "\n")
        }
        sb.WriteString("</modified-files>\n")
    }
    return sb.String()
}
```

## 5.5 Cut point algorithm (`pkg/compaction/cutpoint.go`)

### Valid cut points

A valid cut point is an entry where we can split the conversation without orphaning a tool result. Valid types:

- `EntryTypeMessage` with role `"user"`
- `EntryTypeMessage` with role `"assistant"`
- `EntryTypeCustomMessage`
- `EntryTypeBashExecution`
- `EntryTypeBranchSummary`
- `EntryTypeCompaction`

Never valid: `EntryTypeMessage` with role `"toolResult"`.

```go
// IsValidCutPoint returns true if an entry is a safe place to split the conversation.
func IsValidCutPoint(entry store.Entry) bool {
    switch entry.Type {
    case store.EntryTypeCustomMessage, store.EntryTypeBashExecution,
         store.EntryTypeBranchSummary, store.EntryTypeCompaction:
        return true
    case store.EntryTypeMessage:
        var md store.MessageData
        if err := json.Unmarshal(entry.Data, &md); err != nil {
            return false
        }
        return md.Role == "user" || md.Role == "assistant"
    default:
        return false
    }
}
```

### Find turn start

```go
// FindTurnStartIndex walks backwards from the given index to find the user or
// bashExecution message that started the current turn.
// Returns the index of the turn-starting entry, or -1 if not found.
func FindTurnStartIndex(entries []store.Entry, fromIndex int) int {
    for i := fromIndex; i >= 0; i-- {
        switch entries[i].Type {
        case store.EntryTypeBashExecution:
            return i
        case store.EntryTypeMessage:
            var md store.MessageData
            if err := json.Unmarshal(entries[i].Data, &md); err != nil {
                continue
            }
            if md.Role == "user" {
                return i
            }
        }
    }
    return -1
}
```

### Cut point result

```go
type CutPointResult struct {
    // Index of the first entry to keep in the post-compaction context.
    CutIndex int

    // If the cut splits a multi-message turn, TurnStartIndex is the index
    // of the user message that started the turn. -1 if not a split turn.
    TurnStartIndex int

    // IsSplitTurn is true when the cut falls in the middle of a turn
    // (i.e., the cut point is not itself a user message).
    IsSplitTurn bool
}
```

### Find cut point

```go
// FindCutPoint walks backwards from the end of entries, accumulating estimated
// token sizes. Once accumulated tokens >= keepRecentTokens, it finds the nearest
// valid cut point at or after that index.
//
// Also scans backwards from the cut point to include non-message entries
// (settings changes like model_change, thinking_level_change) that should
// not be summarized away.
func FindCutPoint(entries []store.Entry, messages []agent.AgentMessage, settings CompactionSettings) CutPointResult
```

Algorithm:

```go
func FindCutPoint(entries []store.Entry, messages []agent.AgentMessage, settings CompactionSettings) CutPointResult {
    // Build entry-to-message index for token estimation.
    // Only message entries have corresponding AgentMessages.

    // 1. Walk backwards accumulating tokens until >= keepRecentTokens
    accumulated := 0
    targetIdx := 0
    for i := len(entries) - 1; i >= 0; i-- {
        if entries[i].Type == store.EntryTypeMessage {
            // Find corresponding message, estimate tokens
            accumulated += estimateEntryTokens(entries[i])
        }
        if accumulated >= settings.KeepRecentTokens {
            targetIdx = i
            break
        }
    }

    // 2. Find nearest valid cut point at or after targetIdx
    cutIdx := targetIdx
    for cutIdx < len(entries) {
        if IsValidCutPoint(entries[cutIdx]) {
            break
        }
        cutIdx++
    }
    // If no valid cut found after targetIdx, search before it
    if cutIdx >= len(entries) {
        for cutIdx = targetIdx - 1; cutIdx >= 0; cutIdx-- {
            if IsValidCutPoint(entries[cutIdx]) {
                break
            }
        }
    }

    // 3. Scan backwards from cut to include non-message entries (settings changes)
    for cutIdx > 0 {
        prev := entries[cutIdx-1]
        if prev.Type == store.EntryTypeThinkingChange || prev.Type == store.EntryTypeModelChange {
            cutIdx--
        } else {
            break
        }
    }

    // 4. Detect split turn
    result := CutPointResult{CutIndex: cutIdx, TurnStartIndex: -1}
    if cutIdx < len(entries) {
        entry := entries[cutIdx]
        if entry.Type == store.EntryTypeMessage {
            var md store.MessageData
            if err := json.Unmarshal(entry.Data, &md); err == nil && md.Role != "user" {
                result.IsSplitTurn = true
                result.TurnStartIndex = FindTurnStartIndex(entries, cutIdx-1)
            }
        }
    }

    return result
}
```

## 5.6 Conversation serialization (`pkg/compaction/utils.go`)

```go
// SerializeConversation converts messages to text format for the summarization LLM.
// Format:
//   [User]: <text>
//   [Assistant]: <text>
//   [Assistant thinking]: <thinking>
//   [Assistant tool calls]: <name>(<args>)
//   [Tool result]: <content, truncated to 2000 chars>
func SerializeConversation(messages []agent.AgentMessage) string
```

Implementation:

```go
const maxToolResultChars = 2000

func SerializeConversation(messages []agent.AgentMessage) string {
    var sb strings.Builder
    for _, msg := range messages {
        switch m := msg.(type) {
        case *ai.UserMessage:
            sb.WriteString("[User]: ")
            for _, c := range m.Content {
                if tc, ok := c.(*ai.TextContent); ok {
                    sb.WriteString(tc.Text)
                }
            }
            sb.WriteString("\n\n")

        case *ai.AssistantMessage:
            for _, c := range m.Content {
                switch v := c.(type) {
                case *ai.TextContent:
                    sb.WriteString("[Assistant]: ")
                    sb.WriteString(v.Text)
                    sb.WriteString("\n\n")
                case *ai.ThinkingContent:
                    sb.WriteString("[Assistant thinking]: ")
                    sb.WriteString(v.Thinking)
                    sb.WriteString("\n\n")
                case *ai.ToolCall:
                    sb.WriteString("[Assistant tool calls]: ")
                    args, _ := json.Marshal(v.Arguments)
                    sb.WriteString(fmt.Sprintf("%s(%s)", v.Name, string(args)))
                    sb.WriteString("\n\n")
                }
            }

        case *ai.ToolResultMessage:
            sb.WriteString("[Tool result]: ")
            text := extractToolResultText(m)
            if len(text) > maxToolResultChars {
                text = text[:maxToolResultChars] + "... (truncated)"
            }
            sb.WriteString(text)
            sb.WriteString("\n\n")
        }
    }
    return sb.String()
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
```

## 5.7 Prompts (`pkg/compaction/prompts.go`)

All prompts are exported constants so tests can verify them.

### Summarization system prompt

```go
const SummarizationSystemPrompt = `You are a context summarization assistant. Your task is to read a conversation between a user and an AI coding assistant and produce a structured summary. Do NOT continue the conversation. Do NOT add commentary. Only output the structured summary.`
```

### Initial summarization prompt

```go
const SummarizationPrompt = `Summarize this conversation as a structured checkpoint. Use this exact format:

## Goal
What is the user trying to achieve?

## Constraints & Preferences
Any stated requirements, preferences, or boundaries.

## Progress
- Done: What has been completed.
- In Progress: What is currently being worked on.
- Blocked: What is stuck and why.

## Key Decisions
Important choices made during the conversation and their rationale.

## Next Steps
What should be done next.

## Critical Context
Anything essential for continuing the work that doesn't fit above (variable names, file paths, error messages, configuration details).`
```

### Iterative update prompt

Used when a previous compaction summary exists and we need to merge new conversation into it.

```go
const UpdateSummarizationPrompt = `You have a previous summary of an ongoing conversation and a new segment of that conversation. Update the summary to incorporate the new information.

Rules:
- Merge new information into the existing structure. Do not duplicate.
- Update the Progress section: move completed items from "In Progress" to "Done", add new items.
- Update Key Decisions with any new decisions made.
- Update Next Steps based on current state.
- Keep the same structured format.

Previous summary:
%s

New conversation segment follows.`
```

### Turn prefix summary prompt

Used when a cut point splits a turn. The messages from the turn start to the cut point are summarized separately.

```go
const TurnPrefixPrompt = `Summarize the beginning of this conversation turn. This is a partial turn that was split during context compaction. Focus on:

## Original Request
What did the user ask for in this turn?

## Early Progress
What was accomplished in the portion being summarized?

## Context for Suffix
What context does the remaining (unsummarized) portion of this turn need to make sense?`
```

### Branch summary prompt

```go
const BranchSummaryPreamble = `The user explored a different conversation branch before returning here.`

const BranchSummaryPrompt = `Summarize this conversation branch that the user explored before switching back. Use this format:

## Goal
What was the user trying to achieve on this branch?

## Constraints & Preferences
Any stated requirements or preferences.

## Progress
- Done: What was completed.
- In Progress: What was being worked on.
- Blocked: What was stuck.

## Key Decisions
Important choices made on this branch.

## Next Steps
What the user intended to do next (before switching branches).

## Critical Context
Anything from this branch that may be relevant to the branch the user returned to.`
```

## 5.8 Compaction preparation (`pkg/compaction/compaction.go`)

```go
// CompactionPreparation contains everything needed to generate a compaction summary.
type CompactionPreparation struct {
    // Messages to summarize (entries before the cut point, converted to AgentMessages).
    MessagesToSummarize []agent.AgentMessage

    // If the cut splits a turn, these are the messages from turn start to cut point.
    // Summarized separately with TurnPrefixPrompt.
    TurnPrefixMessages []agent.AgentMessage

    // The entry ID of the first kept entry (the cut point).
    FirstKeptEntryID string

    // Total estimated tokens before compaction.
    TokensBefore int

    // File operations accumulated from the messages being summarized.
    FileOps *FileOperations

    // Previous compaction summary for iterative update (empty string if none).
    PreviousSummary string

    // Previous compaction file operations for merging.
    PreviousFileOps *FileOperations

    // Whether this is a split turn.
    IsSplitTurn bool
}

// PrepareCompaction analyzes entries and determines what to summarize.
// Returns nil if compaction should be skipped (e.g., last entry is already a compaction).
func PrepareCompaction(entries []store.Entry, messages []agent.AgentMessage, settings CompactionSettings) *CompactionPreparation
```

Algorithm:

1. Check if the last entry is already a compaction. If so, return nil (skip).
2. Find any previous compaction entry on the branch (for iterative update).
3. Run `FindCutPoint` to determine where to split.
4. Extract messages to summarize: all message entries before the cut point.
5. If iterative (previous compaction exists), the messages to summarize are only those between the previous compaction and the new cut point.
6. Extract file operations from the messages being summarized.
7. If split turn, extract turn prefix messages (from turn start to cut point).
8. Return `CompactionPreparation`.

## 5.9 Summary generation (`pkg/compaction/compaction.go`)

```go
// CompactionResult is the output of a successful compaction.
type CompactionResult struct {
    Summary          string
    FirstKeptEntryID string
    TokensBefore     int
    ReadFiles        []string
    ModifiedFiles    []string
}

// Compact generates a compaction summary using the LLM.
// If the cut splits a turn, generates two summaries in parallel:
//   1. History summary (all messages before the turn)
//   2. Turn prefix summary (turn start to cut point)
// The final summary combines both plus file operations.
func Compact(
    ctx context.Context,
    prep *CompactionPreparation,
    model ai.Model,
    apiKey string,
    settings CompactionSettings,
) (*CompactionResult, error)
```

Implementation outline:

```go
func Compact(ctx context.Context, prep *CompactionPreparation, model ai.Model, apiKey string, settings CompactionSettings) (*CompactionResult, error) {
    maxSummaryTokens := int(float64(settings.ReserveTokens) * 0.8)

    var historySummary, turnPrefixSummary string
    var err error

    if prep.IsSplitTurn {
        // Generate two summaries in parallel
        g, gctx := errgroup.WithContext(ctx)

        g.Go(func() error {
            var genErr error
            historySummary, genErr = generateSummary(gctx, prep.MessagesToSummarize, prep.PreviousSummary, model, apiKey, maxSummaryTokens)
            return genErr
        })

        turnPrefixMaxTokens := int(float64(settings.ReserveTokens) * 0.5)
        g.Go(func() error {
            var genErr error
            turnPrefixSummary, genErr = generateTurnPrefixSummary(gctx, prep.TurnPrefixMessages, model, apiKey, turnPrefixMaxTokens)
            return genErr
        })

        if err = g.Wait(); err != nil {
            return nil, err
        }
    } else {
        historySummary, err = generateSummary(ctx, prep.MessagesToSummarize, prep.PreviousSummary, model, apiKey, maxSummaryTokens)
        if err != nil {
            return nil, err
        }
    }

    // Combine summaries
    finalSummary := historySummary
    if turnPrefixSummary != "" {
        finalSummary = historySummary + "\n\n---\n\n" + "## Current Turn (partial)\n\n" + turnPrefixSummary
    }

    // Merge and append file operations
    allOps := prep.FileOps
    if prep.PreviousFileOps != nil {
        allOps.Merge(prep.PreviousFileOps)
    }
    readOnly, modified := allOps.ComputeFileLists()
    fileOpsText := FormatFileOperations(readOnly, modified)
    if fileOpsText != "" {
        finalSummary += "\n\n" + fileOpsText
    }

    return &CompactionResult{
        Summary:          finalSummary,
        FirstKeptEntryID: prep.FirstKeptEntryID,
        TokensBefore:     prep.TokensBefore,
        ReadFiles:        readOnly,
        ModifiedFiles:    modified,
    }, nil
}
```

### Generate summary

```go
// generateSummary calls the LLM to produce a structured summary.
// If previousSummary is non-empty, uses UpdateSummarizationPrompt for iterative merge.
func generateSummary(
    ctx context.Context,
    messages []agent.AgentMessage,
    previousSummary string,
    model ai.Model,
    apiKey string,
    maxTokens int,
) (string, error) {
    conversation := SerializeConversation(messages)

    var userPrompt string
    if previousSummary != "" {
        userPrompt = fmt.Sprintf(UpdateSummarizationPrompt, previousSummary) + "\n\n" + conversation
    } else {
        userPrompt = SummarizationPrompt + "\n\n" + conversation
    }

    // Build LLM request with SummarizationSystemPrompt as system message
    // and userPrompt as user message.
    // Set maxTokens on the request.
    // Stream response, collect full text.
    // Return the text.
}
```

### Generate turn prefix summary

```go
// generateTurnPrefixSummary summarizes the beginning of a split turn.
func generateTurnPrefixSummary(
    ctx context.Context,
    messages []agent.AgentMessage,
    model ai.Model,
    apiKey string,
    maxTokens int,
) (string, error) {
    conversation := SerializeConversation(messages)
    userPrompt := TurnPrefixPrompt + "\n\n" + conversation

    // Same LLM call pattern as generateSummary but with TurnPrefixPrompt.
}
```

## 5.10 Branch summarization (`pkg/compaction/branch.go`)

```go
// PrepareBranchSummary collects entries for summarizing an abandoned branch.
// Walks from oldLeafID back to the common ancestor with targetID.
// Applies a token budget (keepRecentTokens) walking newest to oldest.
func PrepareBranchSummary(
    db *store.DB,
    oldLeafID string,
    targetID string,
    settings CompactionSettings,
) (*BranchSummaryPreparation, error)

type BranchSummaryPreparation struct {
    Messages []agent.AgentMessage
    FileOps  *FileOperations
    FromID   string // the old leaf ID
}
```

Algorithm:

1. Use `db.GetBranchEntries(oldLeafID, targetID)` to get entries on the abandoned branch segment.
2. Walk entries newest to oldest with a token budget (keepRecentTokens), collecting messages and file operations.
3. Reverse to chronological order.
4. Return preparation.

```go
// SummarizeBranch generates a summary of an abandoned branch.
func SummarizeBranch(
    ctx context.Context,
    prep *BranchSummaryPreparation,
    model ai.Model,
    apiKey string,
    settings CompactionSettings,
) (*BranchSummaryResult, error)

type BranchSummaryResult struct {
    Summary       string
    ReadFiles     []string
    ModifiedFiles []string
}
```

Implementation:

```go
func SummarizeBranch(ctx context.Context, prep *BranchSummaryPreparation, model ai.Model, apiKey string, settings CompactionSettings) (*BranchSummaryResult, error) {
    conversation := SerializeConversation(prep.Messages)
    userPrompt := BranchSummaryPrompt + "\n\n" + conversation

    maxTokens := int(float64(settings.ReserveTokens) * 0.8)

    // LLM call with SummarizationSystemPrompt as system, userPrompt as user
    summary, err := callLLM(ctx, SummarizationSystemPrompt, userPrompt, model, apiKey, maxTokens)
    if err != nil {
        return nil, err
    }

    // Prepend branch preamble
    summary = BranchSummaryPreamble + "\n\n" + summary

    // Append file operations
    readOnly, modified := prep.FileOps.ComputeFileLists()
    fileOpsText := FormatFileOperations(readOnly, modified)
    if fileOpsText != "" {
        summary += "\n\n" + fileOpsText
    }

    return &BranchSummaryResult{
        Summary:       summary,
        ReadFiles:     readOnly,
        ModifiedFiles: modified,
    }, nil
}
```

## 5.11 LLM call helper (`pkg/compaction/llm.go`)

A shared helper for making summarization LLM calls. Keeps the compaction package from directly constructing provider-specific request formats.

```go
// callLLM sends a single-turn request to the LLM and returns the full response text.
// Used by all summarization functions. Streams the response and collects the text.
func callLLM(
    ctx context.Context,
    systemPrompt string,
    userMessage string,
    model ai.Model,
    apiKey string,
    maxTokens int,
) (string, error) {
    aiCtx := ai.Context{
        Model:        model,
        APIKey:       apiKey,
        SystemPrompt: systemPrompt,
        Messages: []ai.Message{
            &ai.UserMessage{
                Role:    "user",
                Content: []ai.Content{&ai.TextContent{Type: "text", Text: userMessage}},
            },
        },
    }
    opts := &ai.StreamOptions{MaxTokens: maxTokens}
    stream := ai.Stream(model, aiCtx, opts)

    var result strings.Builder
    for event := range stream.Events {
        if te, ok := event.(ai.TextEvent); ok {
            result.WriteString(string(te))
        }
    }

    res := <-stream.Result
    if res.ErrorMessage != "" {
        return "", fmt.Errorf("summarization LLM error: %s", res.ErrorMessage)
    }

    return result.String(), nil
}
```

## 5.12 Tests

### Token estimation tests (`pkg/compaction/tokens_test.go`)

- `EstimateTokens`: text message returns len/4
- `EstimateTokens`: image content returns 1200
- `EstimateTokens`: tool call estimates based on stringified args
- `EstimateTokens`: mixed content sums correctly
- `CalculateContextTokens`: uses TotalTokens when available
- `CalculateContextTokens`: falls back to sum of components
- `EstimateContextTokens`: uses usage from last assistant message + estimates trailing
- `EstimateContextTokens`: no usage data, estimates all messages
- `EstimateContextTokens`: empty message list returns 0

### Trigger tests (`pkg/compaction/trigger_test.go`)

- `ShouldCompact`: under threshold returns false
- `ShouldCompact`: at threshold returns false
- `ShouldCompact`: over threshold returns true
- `ShouldCompact`: disabled settings returns false
- `IsContextOverflow`: matches Anthropic patterns
- `IsContextOverflow`: matches OpenAI patterns
- `IsContextOverflow`: matches Google patterns
- `IsContextOverflow`: excludes rate limit errors
- `IsContextOverflow`: silent overflow (usage.Input > contextWindow)
- `IsContextOverflow`: empty string returns false

### Cut point tests (`pkg/compaction/cutpoint_test.go`)

- `IsValidCutPoint`: user message is valid
- `IsValidCutPoint`: assistant message is valid
- `IsValidCutPoint`: tool result is never valid
- `IsValidCutPoint`: custom message is valid
- `IsValidCutPoint`: model change is not valid
- `FindTurnStartIndex`: finds user message
- `FindTurnStartIndex`: returns -1 for no user message found
- `FindCutPoint`: simple conversation, cut keeps keepRecentTokens worth of context
- `FindCutPoint`: never cuts at tool result (advances to next valid point)
- `FindCutPoint`: includes preceding settings changes in kept portion
- `FindCutPoint`: split turn detection (cut falls on assistant after multi-message turn)
- `FindCutPoint`: all entries within keepRecentTokens, no cut needed (returns 0)

### File operations tests (`pkg/compaction/fileops_test.go`)

- `ExtractFileOpsFromMessage`: read tool extracted
- `ExtractFileOpsFromMessage`: write tool extracted
- `ExtractFileOpsFromMessage`: edit tool extracted
- `ExtractFileOpsFromMessage`: non-tool message returns empty
- `ComputeFileLists`: read-only vs modified separation
- `ComputeFileLists`: file read then edited goes to modified only
- `FormatFileOperations`: XML format correct
- `FormatFileOperations`: empty lists produce empty string
- `Merge`: combines two FileOperations

### Serialization tests (`pkg/compaction/utils_test.go`)

- `SerializeConversation`: user message format
- `SerializeConversation`: assistant text format
- `SerializeConversation`: assistant thinking format
- `SerializeConversation`: tool call format with args
- `SerializeConversation`: tool result truncated to 2000 chars
- `SerializeConversation`: full conversation round-trip

### Compaction pipeline tests (`pkg/compaction/compaction_test.go`)

Uses a faux LLM provider that returns canned summaries.

- `PrepareCompaction`: skip when last entry is compaction
- `PrepareCompaction`: identifies messages to summarize
- `PrepareCompaction`: iterative compaction finds previous summary
- `PrepareCompaction`: split turn extracts turn prefix messages
- `Compact`: produces summary with correct structure
- `Compact`: iterative compaction includes previous summary in prompt
- `Compact`: split turn generates two summaries in parallel
- `Compact`: file operations appended as XML
- `Compact`: merged file operations from previous compaction

### Branch summarization tests (`pkg/compaction/branch_test.go`)

Uses in-memory SQLite and faux LLM.

- `PrepareBranchSummary`: collects entries from abandoned branch segment
- `PrepareBranchSummary`: respects token budget
- `SummarizeBranch`: summary prefixed with branch preamble
- `SummarizeBranch`: file operations appended

### Integration tests (`pkg/compaction/integration_test.go`)

End-to-end test with in-memory SQLite:

- Create session with 50 entries, trigger compaction, verify compaction entry stored
- Verify BuildContext after compaction returns summary + kept messages
- Verify iterative compaction merges summaries
- Verify overflow detection triggers compaction recovery flow

### Verification criteria

- [ ] `go test ./pkg/compaction/...` passes
- [ ] `go test ./pkg/store/...` passes
- [ ] Cut point never falls on a tool result message
- [ ] Split turn detection correctly identifies turn boundaries
- [ ] File operations accumulate across iterative compactions
- [ ] Summary prompts follow the structured format (Goal, Constraints, Progress, Key Decisions, Next Steps, Critical Context)
- [ ] Iterative compaction uses UpdateSummarizationPrompt with previous summary
- [ ] Turn prefix uses separate prompt with reduced token budget (50% of reserveTokens)
- [ ] History summary uses 80% of reserveTokens as maxTokens
- [ ] Branch summary uses BranchSummaryPreamble prefix
- [ ] Overflow patterns exclude rate limiting errors
- [ ] Silent overflow detected via usage.Input > contextWindow
- [ ] Compaction skipped when last entry is already a compaction
- [ ] Context building after compaction emits summary as synthetic user message
- [ ] Concurrent summary generation (split turn) uses errgroup
