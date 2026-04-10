# gcode: project overview

A Go-based coding agent. Uses pi as inspiration but diverges where it makes sense. Optimized for speed, resource efficiency, and batteries-included tooling. Built for power users who want control and visibility.

## Design decisions

| Decision | Choice | Rationale |
|---|---|---|
| Go version | 1.24+ | Full access to generics, range-over-func iterators |
| LLM communication | Raw HTTP + SSE | No SDK dependencies, smaller binary, no version churn |
| LLM providers | Anthropic, OpenAI, Google (day one) | OpenAI compat layer covers the long tail |
| Agent loop | Single loop, subagent-ready interfaces | Ship simple, don't paint into a corner |
| Core tools | read, bash, edit, write, ask_user, fetch | Proven set plus structured user interaction and URL fetching |
| TUI | Custom from scratch | Full control over rendering and liveness signals |
| Storage | SQLite for everything | Sessions, memory, codebase index in one embedded store |
| Extensibility | Skills (markdown) + subprocess/RPC plugins | Language-agnostic plugins, no Go plugin fragility |
| Config | `~/.gcode/` global, `.gcode/` per project | Simple, familiar pattern |
| Streaming | Token-by-token to TUI | Responsive feel, essential for liveness |
| Extended thinking | Supported and visible from day one | Thinking tokens shown in TUI, configurable budget |
| CLI modes | Interactive TUI + pipe mode | `gcode` for interactive, `echo "q" \| gcode -p "prompt"` for scripts |
| Binary | Single static binary | Shells out to git and common unix tools, doesn't bundle them |

## Project structure

```
gcode/
├── cmd/
│   └── gcode/
│       └── main.go                  # CLI entry point
├── pkg/
│   ├── ai/                          # LLM streaming abstraction
│   │   ├── types.go                 # Core types (Model, Context, Message, etc.)
│   │   ├── event_stream.go          # Push-based channel stream
│   │   ├── registry.go              # Provider registry
│   │   ├── stream.go                # Public stream()/complete() API
│   │   ├── cost.go                  # Token cost calculation
│   │   ├── json_parse.go            # Streaming partial JSON parser
│   │   ├── transform.go             # Cross-provider message normalization
│   │   └── providers/
│   │       ├── anthropic.go         # Anthropic Messages API
│   │       ├── openai.go            # OpenAI Completions API
│   │       ├── google.go            # Google Gemini API
│   │       └── compat.go            # OpenAI compat flags
│   ├── agent/                       # Agent runtime
│   │   ├── types.go                 # AgentState, AgentTool, AgentEvent
│   │   ├── agent.go                 # Agent struct, run/steer/followUp
│   │   ├── loop.go                  # Turn cycle, tool dispatch
│   │   ├── events.go                # Liveness events (thinking/executing/idle/stalled)
│   │   └── queue.go                 # PendingMessageQueue
│   ├── tui/                         # Terminal UI framework
│   │   ├── types.go                 # Component, Focusable interfaces
│   │   ├── tui.go                   # TUI struct, differential renderer
│   │   ├── terminal.go              # Raw terminal mode, input parsing
│   │   ├── keys.go                  # Key parsing (Kitty + legacy)
│   │   ├── keybindings.go           # Configurable keybinding system
│   │   ├── width.go                 # ANSI-aware string width
│   │   └── components/
│   │       ├── container.go
│   │       ├── text.go
│   │       ├── editor.go            # Multi-line editor
│   │       ├── markdown.go          # ANSI markdown renderer
│   │       ├── select_list.go       # Fuzzy-filterable list
│   │       ├── box.go               # Border container
│   │       ├── loader.go            # Liveness-aware loader
│   │       ├── status_bar.go        # Agent state + elapsed time
│   │       └── spacer.go
│   ├── tools/                       # Built-in coding tools
│   │   ├── read.go
│   │   ├── bash.go
│   │   ├── edit.go
│   │   ├── write.go
│   │   ├── ask_user.go              # Structured user prompts
│   │   ├── fetch.go                 # URL fetching
│   │   ├── truncate.go              # Output truncation
│   │   ├── diff.go                  # Diff engine
│   │   └── mutation_queue.go        # File mutation serialization
│   ├── store/                       # SQLite storage layer
│   │   ├── db.go                    # Connection, migrations
│   │   ├── session.go               # Session persistence
│   │   └── context.go               # Build LLM context from session
│   ├── compaction/                   # Context window management
│   │   ├── compaction.go            # Session compaction
│   │   ├── branch.go                # Branch summarization
│   │   └── utils.go                 # Shared utilities
│   └── plugin/                      # Plugin system
│       ├── types.go                 # Plugin interface
│       ├── subprocess.go            # Subprocess/RPC plugin host
│       └── skill.go                 # Markdown skill loader
├── internal/
│   ├── prompt/                      # System prompt construction
│   │   └── builder.go
│   └── config/                      # Settings, auth, model registry
│       ├── settings.go
│       ├── auth.go
│       └── models.go
├── go.mod
├── go.sum
└── Makefile
```

## Build order (phases)

Each phase produces a testable, runnable artifact. Complete and verify each phase before moving to the next.

```
Phase 1: pkg/ai          (LLM streaming, all 3 providers + OpenAI compat)
Phase 2: pkg/agent       (agent loop, tool dispatch, liveness events)
Phase 3: pkg/tools       (read, bash, edit, write, ask_user, fetch)
Phase 4: pkg/store       (SQLite session persistence)
Phase 5: pkg/compaction  (context window management)
Phase 6: pkg/tui         (custom terminal UI with liveness display)
Phase 7: cmd/gcode       (CLI wiring, interactive + pipe modes)
Phase 8: pkg/plugin      (skills + subprocess plugins)
Phase 9: Integration     (system prompt, settings, polish)
```

## Dependency graph

```
pkg/ai        (no internal deps)
    │
    v
pkg/agent     (depends on: pkg/ai)
    │
    ├──> pkg/tools      (depends on: pkg/agent for AgentTool interface)
    │
    v
pkg/store     (depends on: pkg/agent for message types)
    │
    v
pkg/compaction (depends on: pkg/ai, pkg/store)

pkg/tui       (no internal deps, standalone)

pkg/plugin    (depends on: pkg/agent for AgentTool interface)

cmd/gcode     (depends on: everything)
```

## Key Go design decisions

### Channels replace AsyncIterable

pi uses `EventStream<T, R>` (push-based async iterable). In Go, use channels:

```go
type EventStream[T any, R any] struct {
    Events  <-chan T       // Consumer reads from this
    events  chan T         // Producer writes to this (same channel, different capabilities)
    result  chan R         // Single-value result channel
    done    chan struct{}  // Close signal
}
```

### Interfaces replace declaration merging

pi uses TypeScript declaration merging for `CustomAgentMessages`. Go uses interfaces:

```go
type AgentMessage interface {
    MessageRole() string
    MessageTimestamp() int64
}
// UserMessage, AssistantMessage, ToolResultMessage all implement AgentMessage
```

### JSON tags replace TypeBox schemas

pi uses TypeBox for tool parameter schemas. Go uses struct tags + `encoding/json`:

```go
type ReadParams struct {
    Path   string `json:"path"`
    Offset *int   `json:"offset,omitempty"`
    Limit  *int   `json:"limit,omitempty"`
}
```

Tool parameter JSON schemas are generated from Go structs at registration time using reflection or a lightweight schema generator.

### Liveness events

The agent loop emits structured liveness events so the TUI always knows the agent's state:

```go
type AgentStatus int
const (
    StatusIdle AgentStatus = iota
    StatusThinking        // LLM inference in flight
    StatusExecuting       // Tool call running
    StatusStalled         // Exceeded duration threshold
)

type LivenessEvent struct {
    Status    AgentStatus
    ToolName  string        // Which tool (when StatusExecuting)
    Elapsed   time.Duration // Time in current state
}
```

The TUI consumes these to show: what the agent is doing, which tool is running, how long it's been running, and escalates visually when a threshold is exceeded.

### Error handling

Errors are sent as events on the channel, never returned from the stream function:

```go
func Stream(model Model, ctx Context, opts *StreamOptions) *AssistantMessageEventStream
```

Errors appear as `EventError` on the events channel.

### SQLite storage

Sessions, compaction state, and future memory/index data all live in SQLite. One `~/.gcode/gcode.db` for global data, one `.gcode/project.db` per project.

```go
import "github.com/mattn/go-sqlite3" // or modernc.org/sqlite for pure Go
```

Pure Go SQLite (modernc.org/sqlite) preferred for zero CGo dependency, keeping the single-binary story clean.

## External dependencies (minimal)

| Dependency | Purpose |
|---|---|
| `golang.org/x/term` | Raw terminal mode |
| `github.com/mattn/go-runewidth` | East Asian character width |
| `github.com/alecthomas/chroma/v2` | Syntax highlighting for markdown |
| `github.com/sergi/go-diff` | Unified diff generation |
| `github.com/google/uuid` | Session/entry IDs |
| `modernc.org/sqlite` | Pure Go SQLite (no CGo) |

No LLM SDK dependencies. All provider communication is raw HTTP + SSE parsing.

## Testing strategy

Each phase has its own tests. Run tests and verify they pass before moving on.

- `pkg/ai`: Faux provider (in-memory, returns canned responses). Integration tests with real APIs behind build tags (`//go:build integration`).
- `pkg/agent`: Faux provider + mock tools. Test the turn cycle, queue draining, liveness event emission.
- `pkg/tools`: Real filesystem operations in `t.TempDir()`. Test truncation, diff, edit with edge cases.
- `pkg/tui`: `VirtualTerminal` (in-memory terminal for testing render output without a real TTY).
- `pkg/store`: In-memory SQLite (`:memory:`) for fast tests. Schema migration tests with temp files.
- `pkg/plugin`: Mock subprocess plugins. Test RPC protocol, tool registration, error handling.

## Detailed phase specs

- `specs/go-agent-phase1-ai.md`: LLM streaming abstraction
- `specs/go-agent-phase2-agent.md`: Agent runtime
- `specs/go-agent-phase3-tools.md`: Built-in tools
- `specs/go-agent-phase4-5-session-compaction.md`: Session persistence and context window management
- `specs/go-agent-phase6-tui.md`: Terminal UI framework
- `specs/go-agent-phase7-8-cli-integration.md`: CLI wiring and integration

## Backlog

See `specs/backlog.md` for future work: codebase comprehension (roam/MemPalace), memory inspector, native token compression (RTK-style), MCP support, intelligent model routing.
