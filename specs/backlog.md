# Backlog

Unscoped ideas and future work. Items here are not yet planned or prioritized.

---

## ⚠️ Agent liveness and progress visibility

**Priority: important**

Existing agents give no signal distinguishing "thinking hard" from "hung". The user stares at a spinner with no way to know if something useful is happening. This is a significant UX failure for long-running tasks.

**What we need:**
- A clear distinction between: idle, thinking (LLM inference in flight), executing (tool call running), and stuck/stalled
- Some form of progress signal during tool execution — at minimum, which tool is running and for how long
- A heuristic or explicit timeout threshold after which the UI escalates the signal (e.g. "still running, 45s elapsed")
- Possibly: streaming partial tool output or LLM token stream so the user sees forward motion

**Open questions:**
- Is this purely a TUI concern (Phase 6), or does the agent loop (Phase 2) need to emit structured liveness events?
- Should the agent emit heartbeat events on a timer during long tool calls?
- What is the right escalation UX: color change, separate status line, elapsed timer, or something else?

---

## Codebase comprehension and memory (roam / MemPalace)

**References:**
- https://github.com/Cranot/roam-code
- https://github.com/milla-jovovich/mempalace

Both tools tackle the problem of giving an agent durable, structured knowledge about a codebase or context beyond what fits in a single context window. We want to understand whether gcode should depend on one of these, bundle/embed one, or implement equivalent functionality natively.

**What roam does:**
Pre-indexes a codebase into a semantic graph (symbols, call graphs, dependency layers, git history) stored in a local SQLite DB. Exposes a CLI with ~139 commands: blast radius analysis, affected tests, health scoring, pre-change safety checks (`preflight`), dead code detection, PR risk scoring, refactor simulation. Output is token-budgetable and LLM-optimized. Designed to replace repeated grep/read cycles.

**What MemPalace does:**
MemPalace (https://github.com/milla-jovovich/mempalace) — repo appears private or not yet public at time of writing. Likely a persistent memory/knowledge store for agents: structured recall of facts, decisions, and context across sessions. Needs investigation once accessible.

**Open questions:**
- Depend on roam as an optional external tool (current approach in global AGENTS.md) vs. embed a Go port of its core indexing/query logic natively in gcode?
- Is the symbol graph useful enough to justify the indexing overhead for gcode's own use cases, or is it overkill for a coding agent harness?
- MemPalace: is it complementary to roam (memory vs. graph) or overlapping? Once the repo is accessible, compare scope.
- If we replicate: what is the minimal useful subset? Full call-graph indexing is expensive; blast radius + affected tests + symbol lookup may cover 80% of the value.
- SQLite is the obvious embedded store for a Go implementation. No external service dependency.

**Likely integration point:** Phase 2 (agent loop) or a new Phase 9. A native index would let gcode answer "what files should I read?" and "what breaks if I change X?" without shelling out to an external tool.

**Related:** If memory/facts are implemented, the TUI needs a way to inspect them. See "Memory and facts inspector" below.

---

## Memory and facts inspector

**Depends on:** Codebase comprehension and memory (roam / MemPalace) backlog item

If gcode implements persistent memory (facts, decisions, context across sessions), the user needs a way to inspect and manage what the agent knows. A black-box memory store is worse than no memory store — the user can't trust what they can't see.

**What we need:**
- A TUI panel (or dedicated view) to browse stored facts and memories
- Ability to view when a fact was recorded, what session/context produced it, and its current confidence or staleness
- Edit and delete individual entries — the agent will be wrong sometimes
- Search/filter across the memory store
- Clear indication in the main agent view when the agent is reading from or writing to memory

**Open questions:**
- Inline panel within the main TUI vs. a separate `gcode memory` subcommand?
- Should memories be scoped per-project, per-user, or both? Needs a clear hierarchy.
- How does this interact with session compaction (Phase 4/5)? Compaction may promote short-term context into long-term memory — that promotion should be visible and reversible.

---

## Intelligent model routing

**Reference:** Amp (https://ampcode.com) routes tasks to different models based on task type rather than using one model for everything.

Not all tasks warrant the same model. A file read + summarize doesn't need the same capability (or cost) as a complex multi-step refactor. Routing intelligently reduces cost and latency without sacrificing quality where it matters.

**What to investigate:**
- How Amp classifies task types and maps them to models. Known pattern: cheaper/faster models for tool calls, retrieval, and short tasks; frontier models for reasoning-heavy steps.
- Whether classification happens at the agent loop level (before dispatch) or per tool call.
- A routing config that lets users define model preferences per task class, with a sensible default hierarchy.
- Dynamic routing: let the agent itself signal "this step needs a stronger model" vs. static rule-based routing.
- Cost tracking per model so the user can see what they're spending and on what.

**Open questions:**
- What are the right task classes for gcode? Candidates: file exploration, code generation, refactoring, explanation, test writing, commit message, PR description.
- Should routing be fully automatic, user-configurable, or both with user config as an override?
- How does this interact with Phase 1 (AI provider abstraction)? The provider layer needs to support multiple simultaneous model configs cleanly.

---

## MCP (Model Context Protocol) support

Pi does not support MCP. gcode should investigate whether and how to support it.

MCP is Anthropic's open protocol for connecting LLMs to external tools and data sources via a standardized client-server interface. Servers expose tools, resources, and prompts; the agent consumes them without bespoke integration code per tool.

**What to investigate:**
- What does first-class MCP support look like in a Go agent? Existing Go MCP client libraries vs. rolling our own.
- Client mode: gcode connects to external MCP servers (filesystem, databases, APIs, custom tools) and exposes their tools to the agent loop natively.
- Server mode: gcode itself exposes an MCP server so other agents or hosts can invoke gcode's tools.
- How MCP tools compose with gcode's native tool system (Phase 3). Are they registered the same way or kept separate?
- Security model: MCP servers run as separate processes; what sandboxing or approval flow do we need?
- Whether supporting MCP gives us roam, RTK, and MemPalace integrations "for free" if those projects ship MCP servers.

**Open questions:**
- MCP or not: gcode already has a native tool system. MCP adds interop at the cost of complexity. Is the ecosystem mature enough to justify it now, or is this a Phase 9+ concern?
- Transport: MCP supports stdio and HTTP/SSE. Which do we prioritize?

---

## Native token compression (RTK-style)

**Reference:** https://github.com/rtk-ai/rtk

RTK ("Rust Token Killer") rewrites CLI/tool output to strip redundant content before it reaches the LLM, achieving 60-90% token reduction transparently. We want an equivalent built into gcode natively rather than relying on an external hook.

**What RTK does:**
- Intercepts tool call results (especially bash/shell output)
- Rewrites/compresses the output: strips ANSI codes, collapses repeated lines, truncates noise, normalizes whitespace, summarizes structured output (JSON, stack traces, test results, etc.)
- Works transparently — the agent sees compressed output, not raw shell output
- Provides meta commands (`rtk gain`, `rtk discover`) to measure savings and find new compression opportunities

**Open questions:**
- Where to apply compression: at the tool result layer (before inserting into context), or at the session compaction layer, or both?
- Rule-based (regex/pattern matching) vs. LLM-assisted summarization for tool output
- Whether to expose compression as a configurable pipeline (per tool type: bash, file read, search results, etc.)
- Metrics: should gcode track token savings and surface them to the user?
- Go implementation options: pure Go rules engine, or delegate heavy summarization to a small local model

**Likely integration point:** Phase 4/5 (session compaction) already handles context reduction. Tool output compression is complementary and operates earlier in the pipeline — at result ingestion time, before content ever enters the session.
