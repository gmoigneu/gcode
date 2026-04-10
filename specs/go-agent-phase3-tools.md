# Phase 3: Built-in tools (`pkg/tools`)

Part of gcode. Go 1.24+.

The six coding tools: read, bash, edit, write, ask_user, fetch. Plus supporting infrastructure for truncation, diffing, and file mutation serialization.

## 3.1 Truncation (`pkg/tools/truncate.go`)

Two truncation strategies matching pi's implementation.

### Constants

```go
const (
    DefaultMaxLines = 2000
    DefaultMaxBytes = 50 * 1024 // 50KB
    GrepMaxLineLength = 500
)
```

### Types

```go
type TruncationResult struct {
    Content             string
    Truncated           bool
    TruncatedBy         string // "lines", "bytes", or ""
    TotalLines          int
    TotalBytes          int
    OutputLines         int
    OutputBytes         int
    LastLinePartial     bool
    FirstLineExceedsLimit bool
    MaxLines            int
    MaxBytes            int
}

type TruncationOptions struct {
    MaxLines int // 0 = use default
    MaxBytes int // 0 = use default
}
```

### TruncateHead (for file reads)

Takes content from the start, stops at limits. Used by the read tool.

```go
func TruncateHead(content string, opts *TruncationOptions) TruncationResult
```

Algorithm:
1. Compute total bytes (`len(content)` for UTF-8) and total lines.
2. If both under limits, return untruncated.
3. If first line alone exceeds byte limit: return empty with `FirstLineExceedsLimit: true`.
4. Iterate lines from start. Track accumulated bytes (line bytes + 1 for newline, except last). Stop when next line would exceed `MaxBytes` or `MaxLines` reached.
5. Join collected lines with `\n`. Return result.

Never returns partial lines.

### TruncateTail (for bash output)

Takes content from the end, stops at limits. Used by the bash tool.

```go
func TruncateTail(content string, opts *TruncationOptions) TruncationResult
```

Algorithm:
1. Same initial checks.
2. Split into lines. Iterate from end (reverse).
3. Track accumulated bytes. Stop when limit hit.
4. **Edge case**: If zero lines collected and current line exceeds byte limit, take the tail of that line (find a valid UTF-8 boundary). Set `LastLinePartial: true`.
5. Return collected lines in correct order.

### Helper

```go
// TruncateLine truncates a single line to maxChars, appending "... [truncated]".
func TruncateLine(line string, maxChars int) string

// FormatSize formats bytes as human-readable (B, KB, MB).
func FormatSize(bytes int) string
```

## 3.2 Diff engine (`pkg/tools/diff.go`)

### Line ending utilities

```go
// DetectLineEnding returns "\r\n" if CRLF appears before a bare LF, else "\n".
func DetectLineEnding(content string) string

// NormalizeToLF replaces \r\n and \r with \n.
func NormalizeToLF(text string) string

// RestoreLineEndings replaces \n with the detected ending.
func RestoreLineEndings(text, ending string) string
```

### BOM handling

```go
type BomResult struct {
    Bom  string // "\xEF\xBB\xBF" or ""
    Text string
}

func StripBom(content string) BomResult
```

### Fuzzy matching

```go
// NormalizeForFuzzyMatch applies progressive normalization:
// 1. Unicode NFKC normalization
// 2. Strip trailing whitespace per line
// 3. Smart quotes to ASCII quotes
// 4. Unicode dashes to ASCII dash
// 5. Special spaces (NBSP, etc.) to regular space
func NormalizeForFuzzyMatch(text string) string

type FuzzyMatchResult struct {
    Found                bool
    Index                int
    ContentForReplacement string // normalized content if fuzzy match was needed
}

// FuzzyFindText tries exact match first, then normalized match.
func FuzzyFindText(content, oldText string) FuzzyMatchResult
```

### Multi-edit application

```go
type EditPair struct {
    OldText string `json:"oldText"`
    NewText string `json:"newText"`
}

type EditResult struct {
    BaseContent string // content used for matching (may be normalized)
    NewContent  string // content after edits applied
}

// ApplyEdits applies multiple search-and-replace edits to content.
// All edits are matched against the original content (not incrementally).
// Returns error if:
// - Any oldText is empty
// - Any oldText is not found
// - Any oldText matches more than once (not unique)
// - Any edits overlap
// - No changes result
func ApplyEdits(normalizedContent string, edits []EditPair, filePath string) (EditResult, error)
```

Algorithm:
1. Normalize all edits' oldText/newText to LF.
2. Validate no empty oldText.
3. Try initial matches. If any need fuzzy matching, use `NormalizeForFuzzyMatch` on the content as baseContent.
4. For each edit: `FuzzyFindText` against baseContent. If not found, return error with edit index. Count occurrences: if > 1, return "not unique" error. Record match position and length.
5. Sort matches by position ascending.
6. Check for overlaps: if `prev.index + prev.length > curr.index`, return overlap error.
7. Apply replacements in reverse order (so indices remain stable).
8. If baseContent == newContent, return "no changes" error.

### Diff generation

Use `github.com/sergi/go-diff/diffmatchpatch` or implement line-level diff.

```go
type DiffResult struct {
    Diff             string
    FirstChangedLine *int
}

// GenerateDiff produces a unified diff with line numbers.
// Format:
//   +N line   (addition, numbered in new file)
//   -N line   (removal, numbered in old file)
//    N line   (context)
func GenerateDiff(oldContent, newContent string, contextLines int) DiffResult
```

Custom format (matching pi): no `---`/`+++` headers. Line numbers inline. Context between changes: if gap <= contextLines*2, show all. Otherwise show leading/trailing context with `...` separator.

## 3.3 File mutation queue (`pkg/tools/mutation_queue.go`)

Serializes concurrent file mutations to the same path. Prevents race conditions when edit and write tools target the same file.

```go
var (
    fileLocks   = make(map[string]*sync.Mutex)
    fileLocksmu sync.Mutex
)

// WithFileMutationQueue runs fn while holding a per-file lock.
func WithFileMutationQueue(path string, fn func() error) error {
    fileLocksmu.Lock()
    lock, ok := fileLocks[path]
    if !ok {
        lock = &sync.Mutex{}
        fileLocks[path] = lock
    }
    fileLocksmu.Unlock()

    lock.Lock()
    defer lock.Unlock()
    return fn()
}
```

## 3.4 Read tool (`pkg/tools/read.go`)

```go
type ReadParams struct {
    Path   string `json:"path" description:"Path to the file to read (relative or absolute)"`
    Offset *int   `json:"offset,omitempty" description:"Line number to start reading from (1-indexed)"`
    Limit  *int   `json:"limit,omitempty" description:"Maximum number of lines to read"`
}

func NewReadTool(cwd string) *agent.AgentTool
```

### Execute algorithm

1. Resolve path to absolute (join with cwd if relative).
2. Check file exists and is readable.
3. Detect if image (by extension: .jpg, .jpeg, .png, .gif, .webp):
   - Read as bytes, base64-encode.
   - Return `[TextContent("image attached"), ImageContent]`.
4. If text file:
   - Read as UTF-8. Split into lines.
   - Apply 1-indexed offset (convert to 0-indexed). Error if offset beyond EOF.
   - If limit provided: slice `[startLine:startLine+limit]`.
   - Apply `TruncateHead` (default 2000 lines / 50KB).
   - Handle edge cases:
     - `FirstLineExceedsLimit`: Return instruction to use bash (`sed -n 'Np' file | head -c N`).
     - Truncated: Append `[Showing lines X-Y of Z. Use offset=N to continue.]`.
     - User-limited but more content: Append `[N more lines in file. Use offset=M to continue.]`.
   - Return content as TextContent.

### Path resolution helper

```go
// ResolveToCwd resolves a path relative to the working directory.
// Returns absolute path. Rejects paths that escape cwd via ../
func ResolveToCwd(path, cwd string) (string, error)
```

Detect traversal attacks: after resolving, verify the result is under cwd (or is an absolute path the user explicitly provided).

## 3.5 Bash tool (`pkg/tools/bash.go`)

```go
type BashParams struct {
    Command string `json:"command" description:"Bash command to execute"`
    Timeout *int   `json:"timeout,omitempty" description:"Timeout in seconds (optional)"`
}

func NewBashTool(cwd string) *agent.AgentTool
```

### Execute algorithm

1. Create command: `exec.CommandContext(ctx, "bash", "-c", command)`.
2. Set `cmd.Dir = cwd`.
3. Set up stdout/stderr pipes (combined).
4. Start command.
5. Read output with rolling buffer:
   - `chunks [][]byte`: Rolling buffer capped at `DefaultMaxBytes * 2`.
   - When total output exceeds `DefaultMaxBytes`, write full output to temp file.
   - Shift old chunks when rolling buffer exceeds cap.
   - Call `onUpdate` with `TruncateTail` applied to rolling buffer for live streaming.
6. Handle timeout: `context.WithTimeout` wrapping the parent context.
7. Wait for command.
8. On completion:
   - Concatenate chunks, apply `TruncateTail`.
   - If truncated: append `[Showing last N lines of M total. Full output: /path]`.
   - Non-zero exit code: return as error result with output + exit code.
   - Success: return output as TextContent.
9. On abort: append "Command aborted" to output.
10. On timeout: append "Command timed out after N seconds".

### Process tree killing

```go
// KillProcessTree kills a process and all its children.
// Uses process groups on Unix (setpgid + kill(-pgid)).
func KillProcessTree(pid int) error
```

Set `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}` so the child gets its own process group. Kill with `syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)`.

## 3.6 Edit tool (`pkg/tools/edit.go`)

```go
type EditParams struct {
    Path  string     `json:"path" description:"Path to the file to edit"`
    Edits []EditPair `json:"edits" description:"One or more targeted replacements"`
}

func NewEditTool(cwd string) *agent.AgentTool
```

### Execute algorithm

1. Validate `edits` array is non-empty.
2. Resolve path to absolute.
3. `WithFileMutationQueue(path, func() error { ... })`:
4. Check file exists and is readable/writable.
5. Read file as UTF-8.
6. `StripBom` (the model won't include invisible BOM in oldText).
7. `DetectLineEnding`.
8. `NormalizeToLF`.
9. `ApplyEdits(normalizedContent, edits, path)` -> `{baseContent, newContent}`.
10. `RestoreLineEndings(newContent, ending)`.
11. Prepend BOM if it was present.
12. Write file.
13. `GenerateDiff(baseContent, newContent, 4)`.
14. Return success message with edit count, diff, and firstChangedLine.

### Legacy compatibility

If the incoming params have top-level `oldText`/`newText` (legacy single-edit format from older models), wrap them into `edits[]`. Check with `PrepareArguments`:

```go
func prepareEditArguments(args map[string]any) map[string]any {
    if _, hasEdits := args["edits"]; hasEdits {
        return args
    }
    if oldText, ok := args["oldText"].(string); ok {
        newText, _ := args["newText"].(string)
        args["edits"] = []map[string]any{{"oldText": oldText, "newText": newText}}
        delete(args, "oldText")
        delete(args, "newText")
    }
    return args
}
```

## 3.7 Write tool (`pkg/tools/write.go`)

```go
type WriteParams struct {
    Path    string `json:"path" description:"Path to the file to write"`
    Content string `json:"content" description:"Content to write to the file"`
}

func NewWriteTool(cwd string) *agent.AgentTool
```

### Execute algorithm

1. Resolve path to absolute.
2. `WithFileMutationQueue(path, func() error { ... })`:
3. Create parent directories: `os.MkdirAll(filepath.Dir(absPath), 0755)`.
4. Write content: `os.WriteFile(absPath, []byte(content), 0644)`.
5. Return `"Successfully wrote N bytes to path"`.

## 3.8 Ask user tool (`pkg/tools/ask_user.go`)

This tool lets the agent ask the user a structured question with optional multiple-choice answers.

```go
type AskUserParams struct {
    Question      string       `json:"question" description:"The question to ask the user"`
    Options       []AskOption  `json:"options,omitempty" description:"List of options for the user to choose from"`
    AllowFreeform *bool        `json:"allowFreeform,omitempty" description:"Add a freeform text option. Default: true"`
    AllowMultiple *bool        `json:"allowMultiple,omitempty" description:"Allow selecting multiple options. Default: false"`
    AllowComment  *bool        `json:"allowComment,omitempty" description:"Collect an optional comment after selecting. Default: false"`
    Context       string       `json:"context,omitempty" description:"Relevant context to show before the question"`
}

type AskOption struct {
    Title       string `json:"title"`
    Description string `json:"description,omitempty"`
}

type AskUserResult struct {
    Selected []string `json:"selected,omitempty"`
    Freeform string   `json:"freeform,omitempty"`
    Comment  string   `json:"comment,omitempty"`
    Cancelled bool    `json:"cancelled"`
}

func NewAskUserTool(handler QuestionHandler) *agent.AgentTool
```

### Execute behavior

- The tool blocks until the user responds via the TUI.
- Uses a callback mechanism: the tool registers a pending question, the TUI renders it (context, options, freeform input), and the user's response is returned.
- If cancelled (e.g. timeout or user dismissal), returns `Cancelled: true`.
- The agent receives the user's selection as a text content response.

This requires a `QuestionHandler` interface that the TUI implements:

```go
type QuestionHandler interface {
    AskUser(params AskUserParams) (AskUserResult, error)
}
```

## 3.9 Fetch tool (`pkg/tools/fetch.go`)

Lets the agent fetch a URL without shelling out to curl.

```go
type FetchParams struct {
    URL     string            `json:"url" description:"URL to fetch"`
    Method  string            `json:"method,omitempty" description:"HTTP method. Default: GET"`
    Headers map[string]string `json:"headers,omitempty" description:"Request headers"`
    Body    string            `json:"body,omitempty" description:"Request body"`
    Timeout *int              `json:"timeout,omitempty" description:"Timeout in seconds. Default: 30"`
}

func NewFetchTool() *agent.AgentTool
```

### Execute behavior

- Makes an HTTP request with the given method, headers, and body.
- Returns the response status code, headers, and body as text.
- Response body is truncated using `TruncateHead` (same limits as read tool: 2000 lines / 50KB).
- For binary content types, returns only status and headers with a note that the body is binary.
- Follows redirects (up to 10).
- Sets a default User-Agent header (`gcode/1.0`).

## 3.10 Tool registration helper

```go
// CodingTools returns the default tool set [read, bash, edit, write, ask_user, fetch].
func CodingTools(cwd string, questionHandler QuestionHandler) []agent.AgentTool {
    return []agent.AgentTool{
        *NewReadTool(cwd),
        *NewBashTool(cwd),
        *NewEditTool(cwd),
        *NewWriteTool(cwd),
        *NewAskUserTool(questionHandler),
        *NewFetchTool(),
    }
}
```

## 3.11 Tests

### Truncation tests (`truncate_test.go`)

- Under limits: no truncation
- Lines limit hit: exactly MaxLines returned, `TruncatedBy: "lines"`
- Bytes limit hit: last complete line before limit, `TruncatedBy: "bytes"`
- First line exceeds limit: `FirstLineExceedsLimit: true`, empty content
- TruncateTail: last N lines returned
- TruncateTail single huge line: partial tail with valid UTF-8 boundary
- Empty content
- Single line content

### Diff tests (`diff_test.go`)

- DetectLineEnding: CRLF file, LF file, mixed (first wins)
- NormalizeToLF: CRLF, CR, mixed
- StripBom: with BOM, without BOM
- FuzzyFindText: exact match, smart quotes, unicode dashes, trailing whitespace, NBSP
- ApplyEdits: single edit, multiple non-overlapping edits, overlapping edits (error), not-found (error), not-unique (error), no-change (error)
- GenerateDiff: single change, multiple changes, context lines, additions, deletions

### Tool tests (use `t.TempDir()`)

- `read_test.go`: Read text file, read with offset/limit, read image, file not found, offset beyond EOF, large file truncation
- `bash_test.go`: Simple command, exit code, timeout, output truncation, process killing
- `edit_test.go`: Single edit, multi-edit, BOM preservation, CRLF preservation, fuzzy match, overlap error, not-found error
- `write_test.go`: New file, overwrite, create parent dirs, large content

### Ask user tests (`ask_user_test.go`)

- Single option selected
- Multiple options selected (with `AllowMultiple: true`)
- Freeform response (no options)
- Cancelled response
- Context is passed through to handler
- Default `AllowFreeform` is true when nil

### Fetch tests (`fetch_test.go`)

Use `httptest.NewServer` for all tests.

- GET request returns status, headers, body
- POST with body and custom headers
- Response body truncation (large response)
- Binary content type returns status/headers only
- Follows redirects
- Timeout handling
- Default User-Agent header set
- Invalid URL returns error

### Verification criteria

- [ ] `go test ./pkg/tools/...` passes
- [ ] Edit tool preserves BOM and line endings
- [ ] Edit tool fuzzy-matches smart quotes and unicode dashes
- [ ] Bash tool kills process tree on timeout/abort
- [ ] Bash tool streams partial output via onUpdate
- [ ] Read tool handles offset/limit with correct continuation messages
- [ ] File mutation queue serializes concurrent edits to same file
- [ ] All tools resolve paths relative to cwd
- [ ] Ask user tool blocks until handler returns
- [ ] Ask user tool returns cancelled result on dismissal
- [ ] Fetch tool truncates large responses
- [ ] Fetch tool handles binary content types
- [ ] Fetch tool respects timeout
