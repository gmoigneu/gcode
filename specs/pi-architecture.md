# pi-mono architecture guide

A deep dive into the pi monorepo: seven packages, one lockstep version, and a layered architecture for building AI coding agents across terminal, web, Slack, and GPU infrastructure.

## Repository at a glance

| Property | Value |
|---|---|
| Repo | `github.com/badlogic/pi-mono` |
| License | MIT |
| Node | >= 20.0.0 |
| Package manager | npm workspaces |
| Linter/formatter | Biome 2.3.5 |
| Compiler | tsgo (native Go port of tsc) |
| Module system | ESM throughout |
| Versioning | Lockstep (all packages share one version) |

## Package overview

| Package | npm name | Purpose |
|---|---|---|
| `packages/ai` | `@mariozechner/pi-ai` | Unified multi-provider LLM streaming API |
| `packages/tui` | `@mariozechner/pi-tui` | Terminal UI framework with differential rendering |
| `packages/agent` | `@mariozechner/pi-agent-core` | Stateful agent runtime with tool execution |
| `packages/coding-agent` | `@mariozechner/pi-coding-agent` | Full coding agent CLI, SDK, and extension system |
| `packages/mom` | `@mariozechner/pi-mom` | Slack bot powered by the coding agent |
| `packages/web-ui` | `@mariozechner/pi-web-ui` | Browser-side chat UI components (Lit web components) |
| `packages/pods` | `@mariozechner/pi` | CLI for managing vLLM deployments on GPU pods |

## Dependency graph

```
                  ┌─────────────┐       ┌─────────────┐
                  │   pi-ai     │       │   pi-tui    │
                  │ LLM abstrac.│       │ Terminal UI │
                  └──────┬──────┘       └──────┬──────┘
                         │                     │
            ┌────────────┼─────────────────────┤
            │            │                     │
            │     ┌──────┴──────┐              │
            │     │ pi-agent-   │              │
            │     │ core        │              │
            │     │ Agent runtm.│              │
            │     └──────┬──────┘              │
            │            │                     │
   ┌────────┼────────────┼─────────────────────┤
   │        │            │                     │
   │  ┌─────┴─────┐ ┌───┴───────────┐  ┌──────┴──────┐
   │  │ pi-web-ui │ │ pi-coding-    │  │  pi-pods    │
   │  │ Browser   │ │ agent         │  │  GPU pod    │
   │  │ chat UI   │ │ CLI + SDK     │  │  manager    │
   │  └───────────┘ └───────┬───────┘  └─────────────┘
   │                        │
   │                 ┌──────┴──────┐
   │                 │   pi-mom    │
   │                 │  Slack bot  │
   └─────────────────┴─────────────┘

   Arrows point from dependent to dependency.
   pi-mom depends on: pi-ai, pi-agent-core, pi-coding-agent
   pi-web-ui depends on: pi-ai, pi-tui
   pi-coding-agent depends on: pi-ai, pi-agent-core, pi-tui
   pi-pods depends on: pi-agent-core
```

Two foundation packages (`pi-ai`, `pi-tui`) have zero internal dependencies. `pi-agent-core` sits in the middle. The four application packages build on top.

Build order mirrors the dependency graph: `tui` → `ai` → `agent` → `coding-agent` → `mom` → `web-ui` → `pods`.

## Layer architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                       APPLICATION LAYER                             │
│                                                                     │
│  pi CLI        Print/JSON    RPC mode     SDK        Slack bot      │
│  Interactive   Headless      JSON-RPC     Programm.  mom            │
│  TUI           stdout        over stdio   embedding                 │
│                                                                     │
│  Web UI                      Pods CLI                               │
│  Browser components          GPU infrastructure                     │
├─────────────────────────────────────────────────────────────────────┤
│                    SESSION LAYER (coding-agent)                      │
│                                                                     │
│  AgentSession ──── Extensions ──── Skills ──── Tools                │
│  Session persist.  TS via jiti     Markdown    read, bash,          │
│  compaction                        instructions edit, write         │
├─────────────────────────────────────────────────────────────────────┤
│                     AGENT LAYER (agent-core)                        │
│                                                                     │
│  Agent loop ──── AgentState ──── Event system ──── Message queues   │
│  Turn cycle      Conversation    Lifecycle hooks   Steering +       │
│  tool dispatch   model, tools                      follow-up        │
├─────────────────────────────────────────────────────────────────────┤
│                       FOUNDATION LAYER                              │
│                                                                     │
│  Streaming API ──── Provider registry    Diff renderer ──── TUI     │
│  Provider-agnostic  Lazy-loaded modules  Line-level        comps    │
│                                          diffing           Editor,  │
│                                                            Markdown │
│                                                            Select.. │
└─────────────────────────────────────────────────────────────────────┘
```

---

## Package deep dives

### pi-ai: unified LLM streaming

The foundation. One API to talk to 20+ LLM providers.

#### Core types

```
Model<TApi>
├── id: string
├── provider: KnownProvider | string
├── api: TApi
├── baseUrl: string
├── contextWindow: number
├── maxOutputTokens: number
├── pricing: Pricing
└── compat?: OpenAICompletionsCompat      (only when TApi = "openai-completions")

Context
├── systemPrompt?: string
├── messages: Message[]
└── tools?: Tool[]

Tool
├── name: string
├── description: string
└── parameters: TSchema                   (TypeBox schema)

AssistantMessageEventStream               (push-based AsyncIterable)
├── push(event)
├── end(result?)
├── result(): Promise<AssistantMessage>
└── [Symbol.asyncIterator]

AssistantMessageEvent                     (discriminated union)
├── start
├── text_start / text_delta / text_end
├── thinking_start / thinking_delta / thinking_end
├── toolcall_start / toolcall_delta / toolcall_end
├── done
└── error
```

#### Provider registry and lazy loading

Providers register at import time but load on first use. The trick: `createLazyStream` returns an `AssistantMessageEventStream` synchronously, triggers the dynamic import in the background, then forwards events from the inner stream to the outer stream. Callers never know it's lazy.

```
  App                  Registry            LazyWrapper          ProviderModule
   │                      │                     │                     │
   │  (import pi-ai)      │                     │                     │
   │  ─ ─ ─ ─ ─ ─ ─ ─ ─ >│                     │                     │
   │                      │ registerBuiltInApiProviders()              │
   │                      │ (10 lazy wrappers, NO imports)            │
   │                      │                     │                     │
   │  stream(model, ctx)  │                     │                     │
   │ ────────────────────>│                     │                     │
   │                      │  streamFn(...)      │                     │
   │                      │────────────────────>│                     │
   │                      │                     │ Create outer stream │
   │<─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ┤ (returned now)     │
   │  AssistantMessageEventStream (empty)       │                     │
   │                      │                     │                     │
   │                      │                     │ import("./anthropic")│
   │                      │                     │────────────────────>│
   │                      │                     │<────────────────────│
   │                      │                     │  Module (cached)    │
   │                      │                     │                     │
   │                      │                     │ stream(model, ctx)  │
   │                      │                     │────────────────────>│
   │                      │                     │<─ ─ inner stream ─ ─│
   │                      │                     │                     │
   │  push(text_delta)    │                     │ forward events      │
   │<─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ┤                     │
   │  push(text_delta)    │                     │                     │
   │<─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ┤                     │
   │  push(done)          │                     │                     │
   │<─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ┤                     │
   │  end()               │                     │                     │
   │<─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ┤                     │
```

This means importing `pi-ai` does not pull in the Anthropic SDK, OpenAI SDK, Google SDK, or any other provider dependency. They load on demand and cache after first use.

#### The `OpenAICompletionsCompat` trick

The `openai-completions` API protocol serves as a universal adapter for 15+ providers (OpenAI, xAI, Groq, Cerebras, OpenRouter, GitHub Copilot, and more). A single `OpenAICompletionsCompat` object encodes 14 behavioral flags that capture provider differences:

| Flag | Controls |
|---|---|
| `supportsDeveloperRole` | `developer` vs `system` role |
| `supportsReasoningEffort` | Whether `reasoning_effort` param works |
| `reasoningEffortMap` | Custom mapping for thinking levels |
| `maxTokensField` | `max_completion_tokens` vs `max_tokens` |
| `requiresToolResultName` | Tool results need `name` field |
| `requiresAssistantAfterToolResult` | Inject assistant message between tool results and user messages |
| `requiresThinkingAsText` | Convert thinking blocks to `<thinking>` delimiters |
| `thinkingFormat` | `openai` / `openrouter` / `zai` / `qwen` variants |
| `supportsStrictMode` | Tool definitions support `strict` mode |
| ... | ... |

`detectCompat()` auto-detects flags from `model.provider` and `model.baseUrl`. Explicit `model.compat` overrides are merged on top field by field. Custom providers only need to set the flags where they diverge.

#### Event stream: push-based async iterable

`EventStream<T, R>` is the core streaming primitive. It bridges push (producer) and pull (consumer) semantics:

- **Push side:** `push(event)` delivers to a waiting consumer or buffers in a queue
- **Pull side:** `[Symbol.asyncIterator]` yields from buffer or suspends via a promise
- **Completion:** `isComplete(event)` predicate detects the terminal event, `end()` forces termination
- **Result:** `result()` returns a `Promise<R>` resolved from the completion event

Errors are never thrown. They're encoded as `error` events with `stopReason: "error"`. This is a deliberate design choice: the stream contract guarantees consumers always get a clean async iteration with no surprise rejections.

---

### pi-tui: terminal UI framework

Standalone terminal rendering library. No dependency on pi-ai or pi-agent-core.

#### Component model

Minimal interface: components return string arrays from `render(width)` and optionally handle keyboard input. No virtual DOM, no reconciliation. Each component is responsible for its own ANSI escape codes.

```
Component (interface)
├── render(width: number): string[]
├── handleInput?(data: string): void
└── invalidate(): void

Focusable (interface)
└── focused: boolean

                  Component
                     │
      ┌──────┬───────┼────────┬──────────┐
      │      │       │        │          │
  Container Text   Editor  Markdown  SelectList
      │                (Focusable)    (Focusable)
      │
     TUI
   (root)
   ├── overlays: Map
   ├── addOverlay(component, options)
   └── requestRender()

  Other components: TruncatedText, Input, Box,
                    Image, SettingsList, Loader, Spacer
```

Built-in components: `Text`, `TruncatedText`, `Input`, `Editor` (multi-line with autocomplete, kill ring, undo), `Markdown` (ANSI-rendered with syntax highlighting), `SelectList` (fuzzy-filterable), `SettingsList`, `Image` (Kitty/iTerm2 protocols), `Box`, `Container`, `Loader`, `Spacer`.

#### Differential rendering

The `doRender()` method implements line-level diffing:

```
  render(width) on component tree
            │
            v
    Composite overlays
            │
            v
   Full redraw needed? ──────────────────────────────────────┐
    │              │                                         │
    │ YES          │ NO                                      │
    │              │                                         │
    │  Triggers:   v                                         │
    │  - 1st render   Compare newLines vs previousLines      │
    │  - Width chg    │                                      │
    │  - Height chg   v                                      │
    │  - Content   Find firstChanged, lastChanged indices    │
    │    above     │                                         │
    │    viewport  v                                         │
    │           ANSI cursor move to firstChanged             │
    v              │                                         │
  Clear screen     v                                         │
  Write all     Rewrite only changed lines                   │
  lines            │                                         │
    │              v                                         │
    │         Old had more lines? ─── YES ──> Erase extras   │
    │              │ NO                          │            │
    │              v                             v            │
    └─────────> Position hardware cursor <───────┘            │
                                   ^                          │
                                   └──────────────────────────┘
```

All output is wrapped in synchronized output escape sequences (`CSI 2026h` / `CSI 2026l`) to prevent tearing. Renders are rate-limited to 60fps via `requestRender()` coalescing.

A hard guard catches width overflow: if any rendered line exceeds terminal width, the TUI writes a crash log and throws. This prevents terminal corruption from going unnoticed.

---

### pi-agent-core: stateful agent runtime

The agent loop, tool execution, state management, and event system. Provider-agnostic: depends only on pi-ai types.

#### Agent lifecycle

```
  App              Agent            pi-ai (stream)      Tools
   │                 │                    │                │
   │  run(userMsg)   │                    │                │
   │────────────────>│                    │                │
   │                 │ emit(agent_start)  │                │
   │                 │                    │                │
   │                 │ ── TURN LOOP ──────────────────────────────
   │                 │                    │                │     │
   │                 │ emit(turn_start)   │                │     │
   │                 │ transformContext() │                │     │
   │                 │ convertToLlm()    │                │     │
   │                 │                    │                │     │
   │                 │ streamSimple(...)  │                │     │
   │                 │───────────────────>│                │     │
   │                 │                    │                │     │
   │                 │ emit(message_start)│                │     │
   │                 │<─ text_delta ──────│                │     │
   │                 │ emit(msg_update)   │                │     │
   │                 │<─ toolcall_delta ──│                │     │
   │                 │ emit(msg_update)   │                │     │
   │                 │<─ done ───────────│                │     │
   │                 │ emit(message_end)  │                │     │
   │                 │                    │                │     │
   │                 │ ── if tool calls ──────────────────────   │
   │                 │                    │                │ │   │
   │                 │ emit(tool_exec_start)               │ │   │
   │                 │ beforeToolCall()   │                │ │   │
   │                 │ execute(params)    │                │ │   │
   │                 │───────────────────────────────────>│ │   │
   │                 │<──────────────────────── result ───│ │   │
   │                 │ afterToolCall()    │                │ │   │
   │                 │ emit(tool_exec_end)│                │ │   │
   │                 │ ──────────────────────────────────────┘   │
   │                 │                    │                │     │
   │                 │ emit(turn_end)     │                │     │
   │                 │                    │                │     │
   │                 │ Check steering queue                │     │
   │                 │ ├─ has msgs: inject, continue loop  │     │
   │                 │ ├─ no tools, no follow-ups: stop    │     │
   │                 │ └─ has follow-ups: inject, continue │     │
   │                 │ ────────────────────────────────────────── │
   │                 │                    │                │
   │                 │ emit(agent_end)    │                │
   │<────────────────│                    │                │
```

#### Message queues

Two independent queues enable mid-run control:

- **Steering queue** (`agent.steer(msg)`): Messages injected after the current assistant turn, before the agent stops. For course corrections while the agent is working.
- **Follow-up queue** (`agent.followUp(msg)`): Messages processed only when the agent would otherwise stop. For post-processing chains.

Both support configurable drain modes: `"all"` (drain entire queue at once) or `"one-at-a-time"` (one message per turn).

#### Custom message types via declaration merging

```typescript
// In your app:
declare module "@mariozechner/pi-agent" {
    interface CustomAgentMessages {
        artifact: ArtifactMessage;
        notification: NotificationMessage;
    }
}

// AgentMessage automatically becomes:
// Message | ArtifactMessage | NotificationMessage
```

The `convertToLlm` hook bridges the gap: it transforms `AgentMessage[]` to `Message[]` at the LLM boundary, filtering out or converting custom types. The agent core never needs to know about app-specific message types.

#### Tool execution

Configurable via `toolExecution: "sequential" | "parallel"` (default: parallel). In parallel mode, tool calls from a single assistant message are preflighted sequentially (via `beforeToolCall`), then executed concurrently, with results emitted in source order.

`AgentTool` extends pi-ai's `Tool` with:
- `label`: Human-readable name
- `execute(toolCallId, params, signal, onUpdate)`: The `onUpdate` callback enables streaming partial results during long-running tool execution
- `prepareArguments`: Optional pre-processing of arguments before execution

---

### pi-coding-agent: the product

The full coding agent. Four run modes, session persistence, extension system, skills, compaction, and an SDK.

#### Run modes

```
                        pi binary
                            │
                       Parse args
                            │
          ┌─────────┬───────┼────────┬──────────┐
          │         │       │        │          │
      Interactive  Print   JSON     RPC        SDK
      Full TUI     Stream  Struct.  JSON-RPC   Programmatic
                   stdout  output   over stdio API
```

#### Built-in tools

| Tool | Description |
|---|---|
| `read` | Read file contents (text and images) with offset/limit |
| `bash` | Execute shell commands with optional timeout |
| `edit` | Precise search-and-replace edits with `oldText`/`newText` pairs |
| `write` | Create or overwrite files, auto-creates parent directories |

Two predefined tool sets:
- **`codingTools`**: `[read, bash, edit, write]` (default)
- **`readOnlyTools`**: `[read, grep, find, ls]` (for read-only subagents)

Supporting infrastructure: file mutation queue (serializes concurrent writes), diff-based edit engine (BOM stripping, line ending normalization), output truncation (2000 lines / 50KB default).

#### Session management

Append-only JSONL with typed entries. Supports tree-based navigation: fork from any point, branch summaries, no history rewriting.

```
  User: Fix the bug
        │
  Assistant: I'll look at...
        │
        ├──────────────────────────┐
        │                          │
  User: Try approach B       User: Also fix the tests
        │                          │
  Assistant: Switching...    Assistant: Running tests...
        │                          │
  User: Perfect              User: Still failing
```

Entry types: `SessionMessageEntry`, `CompactionEntry`, `BranchSummaryEntry`, `ModelChangeEntry`, `ThinkingLevelChangeEntry`, `CustomEntry`. Version-migrated via `migrateSessionEntries`.

#### Compaction

Two strategies for context window management:

**Session compaction**: Triggered when `contextTokens > contextWindow - reserveTokens`. Walks backwards to find a cut point, generates an LLM-powered checkpoint summary with a rigid structure (Goal, Constraints, Progress, Key Decisions, Next Steps, Critical Context). File operations are tracked across compactions.

**Branch summarization**: When navigating between branches, generates a summary of the abandoned branch. Uses the same structured format, with token-budgeted entry collection.

```
  contextTokens > limit?
        │
    YES │              NO
        │               └──> Continue normally
        v
  Find cut point
  (walk backwards, keep ~20K recent tokens)
        │
        v
  Split in middle of turn?
    │            │
   YES           NO
    │            │
    v            │
  Generate       │
  turn prefix    │
  summary        │
    │            │
    v            v
  LLM generates checkpoint summary
        │
        v
  Replace old messages with summary entry
        │
        v
  Append CompactionEntry to JSONL
```

#### Extension system

Extensions are TypeScript modules loaded at runtime via `jiti`. Discovery searches `cwd/.pi/extensions/`, `~/.pi/agent/extensions/`, and configured paths.

```
ExtensionFactory                     (api: ExtensionAPI) => void
    │
    │ receives
    v
ExtensionAPI
├── on(event, handler)               ~30 lifecycle event types
├── registerTool(definition)
├── registerCommand(name, options)
├── registerShortcut(key, options)
├── registerFlag(name, options)
├── registerProvider(name, config)
├── registerMessageRenderer(type, renderer)
├── sendMessage(text)
├── exec(command)
├── setModel(model)
└── events: EventBus                 inter-extension communication

ToolDefinition
├── name: string
├── description: string
├── parameters: TSchema
├── execute(id, params, signal, onUpdate)
├── renderCall?(params): string[]    TUI rendering hooks
├── renderResult?(result): string[]
├── promptSnippet?: string           injected into system prompt
└── promptGuidelines?: string
```

Extensions can register custom tools, commands, shortcuts, flags, message renderers, and LLM providers. They have full access to the agent lifecycle through ~30 event types.

For compiled Bun binaries, extensions use `virtualModules` to resolve `@mariozechner/*` imports. For Node.js development, they use `alias` mappings. This is handled transparently by the loader.

#### Skills

Markdown files (`SKILL.md`) with YAML frontmatter. Loaded from the filesystem and injected into the system prompt. The LLM reads the skill description and decides when to use it.

```yaml
---
name: conventional-commit
description: Generate conventional commit messages
---
# Instructions for the LLM...
```

Skills are not code. They're structured instructions that shape agent behavior. Loaded from `cwd/.pi/agent/skills/`, `~/.pi/agent/skills/`, and configured paths.

#### System prompt construction

The system prompt is assembled from multiple sources, not a static template:

1. Role declaration and available tools
2. Dynamic guidelines based on which tools are enabled
3. Pi documentation references (for self-help)
4. Project context files (e.g., `AGENTS.md`)
5. Loaded skills (if read tool is available)
6. Current date and working directory

---

### pi-mom: Slack bot

A Slack bot that delegates messages to the coding agent. Uses Socket Mode for real-time events.

```
  Slack workspace
        │
        │ Socket Mode
        v
    SlackBot
        │
        v
    MomHandler
        │
        v
    AgentRunner (one per channel)
        │
        ├──> AgentSession (from pi-coding-agent)
        │        │
        │        └──> Agent (from pi-agent-core)
        │                 │
        │                 └──> LLM (via pi-ai)
        │
        └──> Per-channel persistence
             ├── log.jsonl
             ├── context.jsonl
             └── MEMORY.md
```

Key design points:
- One `AgentRunner` per channel, cached across messages
- Per-channel persistence: `log.jsonl` (message history), `context.jsonl` (session state), `MEMORY.md` (working memory)
- All Slack API operations serialized through a promise queue to prevent race conditions
- Events system: watches `events/` directory for JSON event files (immediate, one-shot, periodic with cron)
- `[SILENT]` marker support for suppressing output on periodic no-op events
- Hardcoded to `claude-sonnet-4-5` (not configurable)

---

### pi-web-ui: browser chat components

Reusable web components for AI chat interfaces, built with Lit and Tailwind CSS v4.

```
  Web UI components                Sandbox system
  ─────────────────                ──────────────
  AgentInterface                   SandboxIframe
      │                                │
      v                                v
  ChatPanel                        RuntimeMessageBridge
  ├── MessageList                  ├── ArtifactsRuntime
  ├── Input                        ├── ConsoleRuntime
  ├── ArtifactsPanel ─────────────>└── FileDownloadRuntime
  ├── ModelSelector
  └── SettingsDialog

  Storage (IndexedDB)
  ───────────────────
  ├── Sessions store
  ├── API keys store
  ├── Custom providers store
  └── Settings store
```

Browser-specific concerns:
- `createStreamFn` and `applyProxyIfNeeded` handle CORS by optionally routing LLM requests through a proxy
- Auto-discovery of local providers (Ollama, LM Studio) via their SDKs
- Sandboxed iframe execution for artifacts with a runtime message router
- Tool renderer registry (parallel to coding-agent's tool rendering, adapted for the browser)

---

### pi-pods: GPU infrastructure

CLI for managing vLLM deployments on remote GPU pods. Supports DataCrunch and RunPod providers.

```
  pi-pods CLI
      │
      ├── pods setup ──> Configure SSH
      ├── start <model> ──> Remote GPU pod ──> vLLM server ──> OpenAI-compatible API
      ├── stop ──> Shutdown
      ├── list ──> Running models
      ├── logs ──> Stream output
      └── agent ──> Chat with model (spawns pi-coding-agent subprocess)
```

Predefined model configs for Qwen, GPT-OSS, and GLM families. Multi-GPU support. The `agent` command spawns the coding agent as a subprocess connecting to the remote vLLM endpoint.

---

## Opinionated choices

These are deliberate, non-obvious design decisions baked into the architecture.

### 1. Errors in the stream, never thrown

The `StreamFunction` contract mandates that runtime and network errors are encoded as stream events (`stopReason: "error"`), never thrown as exceptions. Consumers always get a clean async iteration with no surprise rejections.

**Why it matters:** In a system with lazy-loaded providers, forwarded streams, and concurrent tool execution, thrown errors create unpredictable control flow. Stream-encoded errors are always handled in the same code path as normal completion.

### 2. TypeBox over Zod for tool schemas

The entire stack uses `@sinclair/typebox` for tool parameter schemas, validated at runtime with AJV. Not Zod, not JSON Schema directly.

**Why it matters:** TypeBox produces static types and JSON Schema from a single definition. Zod requires `zod-to-json-schema` for the same effect. TypeBox schemas are plain objects, making them serializable for RPC and extension boundaries without transformation.

### 3. Lockstep versioning across all packages

Every release bumps all seven packages to the same version. No independent versioning, no `^` ranges between internal packages (they use exact version matches).

**Why it matters:** Eliminates version matrix headaches in a monorepo where packages have tight coupling. A single version number tells you the exact state of the entire system.

### 4. Tabs with indent width 3

Biome is configured for tab indentation with a display width of 3. Most projects use 2 or 4.

**Why it matters:** This is a pure aesthetic preference. Tab characters with width 3 give a visual indent that's less aggressive than 4 but more readable than 2.

### 5. tsgo instead of tsc

All packages (except web-ui, which targets browsers) compile with `tsgo`, the native Go port of the TypeScript compiler. The root `check` script also uses `tsgo --noEmit` for type checking.

**Why it matters:** tsgo is significantly faster than tsc for large codebases. Using it for both builds and type-checking means the development feedback loop is consistently fast.

### 6. Line-level differential rendering (not character-level)

The TUI renderer compares entire lines as strings. Changed lines are rewritten completely. No character-level patching.

**Why it matters:** Character-level diffing in a terminal with ANSI escape codes is complex and error-prone (escape sequences break substring matching). Line-level diffing is simpler, fast enough for 60fps terminal rendering, and avoids an entire class of rendering bugs.

### 7. Append-only JSONL for session persistence

Sessions are append-only JSONL files with typed entries. History is never rewritten. Branching is modeled as tree navigation with parent pointers, not file mutations.

**Why it matters:** Append-only storage is crash-safe (no partial writes that corrupt history), simple to implement, and naturally supports the branch/fork model without complex data structures.

### 8. Declaration merging for extensibility (not inheritance)

Custom message types are added via TypeScript's declaration merging on `CustomAgentMessages`, not by extending a base class. The `AgentMessage` type union expands automatically.

**Why it matters:** No base class coupling. Custom types don't need to inherit from anything. The agent core remains generic, and the type system enforces correctness at the boundary (the `convertToLlm` hook must handle all custom types).

### 9. Skills are markdown, not code

Skills are `SKILL.md` files with YAML frontmatter. They're injected into the system prompt as instructions for the LLM. No runtime code execution, no API surface.

**Why it matters:** Skills are safe by design (no arbitrary code), portable (just text files), and composable (multiple skills in the system prompt). The LLM decides when and how to apply them. Extensions handle the code side.

### 10. Lazy provider loading with synchronous return

`createLazyStream` returns an `AssistantMessageEventStream` immediately, triggers the dynamic import in the background, and forwards events. The synchronous return signature means callers don't need to know about or handle the lazy loading.

**Why it matters:** Importing pi-ai is fast (no provider SDKs loaded). The first call to a specific provider pays the import cost. Subsequent calls use the cached module. Import failures appear as stream error events, maintaining the "errors in the stream" contract.

### 11. One compat object to support 15+ OpenAI-compatible providers

Instead of writing a separate provider implementation for each OpenAI-compatible service, a single `OpenAICompletionsCompat` object with 14 flags captures all behavioral differences. Auto-detection fills in defaults, explicit overrides take precedence.

**Why it matters:** Adding a new OpenAI-compatible provider is a data change (flags), not a code change. The flag approach scales linearly while separate implementations would scale with duplicated code.

### 12. jiti for runtime TypeScript loading

Extensions are raw TypeScript files loaded at runtime via `jiti` (a Just-In-Time TypeScript transpiler). For compiled Bun binaries, `virtualModules` resolve `@mariozechner/*` imports. For Node.js, `alias` mappings handle it.

**Why it matters:** Extension authors write TypeScript directly, no build step. The dual resolution strategy (virtualModules for Bun, alias for Node) means extensions work identically in both development and production without configuration.

### 13. Biome replaces ESLint + Prettier

A single tool for both linting and formatting. No `.eslintrc`, no `.prettierrc`, no plugin ecosystem.

**Why it matters:** Biome is faster (Rust-based), has zero configuration drift between linting and formatting rules, and eliminates the ESLint/Prettier conflict surface. The tradeoff is a smaller rule ecosystem, but the project doesn't need exotic lint rules.

---

## Data flow: from user prompt to tool execution

```
  User                 pi-coding-agent       pi-agent-core         pi-ai            LLM Provider
   │                        │                     │                   │                   │
   │ "Fix the bug"          │                     │                   │                   │
   │───────────────────────>│                     │                   │                   │
   │                        │ Load session        │                   │                   │
   │                        │ Build system prompt  │                   │                   │
   │                        │ Compact if needed   │                   │                   │
   │                        │                     │                   │                   │
   │                        │ agent.run(userMsg)  │                   │                   │
   │                        │────────────────────>│                   │                   │
   │                        │                     │ transformContext() │                   │
   │                        │                     │ convertToLlm()   │                   │
   │                        │                     │ streamSimple(...) │                   │
   │                        │                     │──────────────────>│                   │
   │                        │                     │                   │ getApiProvider()  │
   │                        │                     │                   │ HTTP stream       │
   │                        │                     │                   │──────────────────>│
   │                        │                     │                   │<── SSE chunks ────│
   │                        │                     │<── events ────────│                   │
   │                        │<── message_update ──│                   │                   │
   │                        │                     │                   │                   │
   │                        │                     │ (assistant requests read tool)        │
   │                        │<─ tool_exec_start ──│                   │                   │
   │                        │ readTool.execute()  │                   │                   │
   │                        │── tool result ─────>│                   │                   │
   │                        │                     │ Append to messages│                   │
   │                        │                     │ streamSimple(...) │                   │
   │                        │                     │──────────────────>│──────────────────>│
   │                        │                     │                   │<─────────────────│
   │                        │                     │                   │                   │
   │                        │                     │ (assistant requests edit tool)        │
   │                        │<─ tool_exec_start ──│                   │                   │
   │                        │ editTool.execute()  │                   │                   │
   │                        │── tool result ─────>│                   │                   │
   │                        │                     │ No more tool calls│                   │
   │                        │                     │ Check queues: empty                   │
   │                        │<── agent_end ───────│                   │                   │
   │                        │ Persist session     │                   │                   │
   │<── Rendered output ────│                     │                   │                   │
```

---

## Extension points summary

| Layer | Extension mechanism | What you can extend |
|---|---|---|
| pi-ai | `registerApiProvider()` | Custom LLM providers |
| pi-ai | `Model.compat` | Behavioral flags for OpenAI-compatible providers |
| pi-agent-core | `CustomAgentMessages` | Custom message types via declaration merging |
| pi-agent-core | `beforeToolCall` / `afterToolCall` | Tool execution interception |
| pi-agent-core | `convertToLlm` / `transformContext` | Context transformation |
| pi-coding-agent | Extension API | Tools, commands, shortcuts, flags, renderers, providers |
| pi-coding-agent | Skills (SKILL.md) | LLM behavioral instructions |
| pi-coding-agent | Prompt templates | System prompt customization |
| pi-coding-agent | Themes | TUI appearance |
| pi-web-ui | `registerToolRenderer()` | Custom tool rendering in browser |
| pi-web-ui | `CustomProvidersStore` | Custom provider configuration |
