# Phase 7: CLI wiring (`cmd/gcode`)

> **Project:** gcode. Go 1.24+.

Connects all packages into the `gcode` binary. Argument parsing, mode dispatch, interactive TUI mode.

## 7.1 CLI entry point (`cmd/gcode/main.go`)

```go
func main() {
    args := parseArgs(os.Args[1:])

    switch {
    case args.PrintMode:
        runPrintMode(args)
    case args.JSONMode:
        runJSONMode(args)
    default:
        runInteractiveMode(args)
    }
}

type Args struct {
    // Model selection
    Model    string // "provider/model-id" or just "model-id"
    Provider string

    // Mode flags
    PrintMode bool
    JSONMode  bool

    // Options
    Prompt      string   // -p "prompt"
    SystemPrompt string  // --system "prompt"
    ContinueSession string // --continue session-id
    Cwd         string   // --cwd directory

    // Thinking
    ThinkingLevel string // --thinking off|minimal|low|medium|high|xhigh

    // Auth
    APIKey string // --api-key or env var
}
```

Use `flag` package or a simple manual parser (avoid heavy CLI frameworks for a learning project).

### API key resolution

```go
func resolveAPIKey(args Args) string {
    if args.APIKey != "" {
        return args.APIKey
    }
    // Check env vars based on provider
    switch {
    case strings.Contains(args.Model, "claude") || args.Provider == "anthropic":
        return os.Getenv("ANTHROPIC_API_KEY")
    case strings.Contains(args.Model, "gpt") || args.Provider == "openai":
        return os.Getenv("OPENAI_API_KEY")
    case strings.Contains(args.Model, "gemini") || args.Provider == "google":
        return os.Getenv("GOOGLE_API_KEY")
    }
    return ""
}
```

### Model resolution

```go
func resolveModel(args Args) (ai.Model, error) {
    // Parse "provider/model-id" format
    if strings.Contains(args.Model, "/") {
        parts := strings.SplitN(args.Model, "/", 2)
        return ai.GetModel(ai.Provider(parts[0]), parts[1])
    }
    // Search all providers for a matching model ID
    for _, provider := range ai.GetProviders() {
        if model, ok := ai.GetModel(provider, args.Model); ok {
            return model, nil
        }
    }
    return ai.Model{}, fmt.Errorf("model not found: %s", args.Model)
}
```

## 7.2 Print mode (`cmd/gcode/print.go`)

Headless mode. Streams LLM output to stdout. No TUI.

Print mode passes `nil` as the `QuestionHandler` since there is no TUI to display interactive prompts. The `ask_user` tool returns an error explaining that interactive mode is required.

```go
func runPrintMode(args Args) {
    model, err := resolveModel(args)
    fatal(err)
    apiKey := resolveAPIKey(args)
    cwd, _ := os.Getwd()
    if args.Cwd != "" { cwd = args.Cwd }

    // Build agent: nil QuestionHandler disables ask_user in pipe mode
    tools := tools.CodingTools(cwd, nil)
    a := agent.New(agent.AgentConfig{
        ToolExecution: agent.ToolExecParallel,
        GetAPIKey:     func(_ ai.Provider) string { return apiKey },
    })
    a.State().Model = model
    a.State().SystemPrompt = prompt.BuildSystemPrompt(cwd, tools)
    a.State().Tools = tools
    if args.ThinkingLevel != "" {
        a.State().ThinkingLevel = ai.ThinkingLevel(args.ThinkingLevel)
    }

    // Subscribe to events for output
    a.Subscribe(func(event agent.AgentEvent, ctx context.Context) {
        switch event.Type {
        case agent.MessageUpdate:
            if event.AssistantMessageEvent != nil {
                if event.AssistantMessageEvent.Type == ai.EventTextDelta {
                    fmt.Print(event.AssistantMessageEvent.Delta)
                }
            }
        case agent.ToolExecutionStart:
            fmt.Fprintf(os.Stderr, "\n[tool: %s]\n", event.ToolName)
        case agent.ToolExecutionEnd:
            if event.ToolIsError {
                fmt.Fprintf(os.Stderr, "[tool error: %s]\n", event.ToolResult.Content)
            }
        }
    })

    // Run
    prompt := args.Prompt
    if prompt == "" {
        // Read from stdin
        data, _ := io.ReadAll(os.Stdin)
        prompt = string(data)
    }
    err = a.Run(prompt)
    if err != nil {
        fmt.Fprintf(os.Stderr, "\nError: %s\n", err)
        os.Exit(1)
    }
    fmt.Println()
}
```

## 7.3 Interactive mode (`cmd/gcode/interactive.go`)

Full TUI with editor, message display, streaming output, and liveness indicators.

```go
func runInteractiveMode(args Args) {
    model, err := resolveModel(args)
    fatal(err)
    apiKey := resolveAPIKey(args)
    cwd, _ := os.Getwd()
    if args.Cwd != "" { cwd = args.Cwd }

    // Initialize terminal
    term := tui.NewTerminal()
    if err := term.Start(); err != nil {
        fatal(err)
    }
    defer term.Stop()

    // Initialize TUI
    ui := tui.New(term)

    // Build UI layout
    app := newInteractiveApp(ui, model, apiKey, cwd, args)
    app.start()

    // Block until app exits
    <-app.doneCh
}
```

### Interactive app

The interactive app implements `tools.QuestionHandler` so the `ask_user` tool can display structured prompts in the TUI.

```go
type interactiveApp struct {
    ui    *tui.TUI
    agent *agent.Agent
    db    *store.DB

    // UI components
    messageList *messageListComponent
    editor      *tui.Editor
    statusBar   *tui.Text
    loader      *tui.Loader

    model     ai.Model
    apiKey    string
    cwd       string
    sessionID string
    leafID    string // tracks current leaf entry for appending
    doneCh    chan struct{}
}

// AskUser implements tools.QuestionHandler. Renders an interactive prompt
// in the TUI and blocks until the user responds.
func (app *interactiveApp) AskUser(params tools.AskUserParams) (tools.AskUserResult, error) {
    // Show select list or freeform input in the TUI, block until answered.
    // Implementation uses tui.SelectList for options, editor for freeform.
    // Returns the user's selection/text.
}

func newInteractiveApp(ui *tui.TUI, model ai.Model, apiKey, cwd string, args Args) *interactiveApp

func (app *interactiveApp) start() {
    // Layout: messages at top, status bar, editor at bottom
    app.messageList = newMessageListComponent()
    app.editor = tui.NewEditor(tui.NewKeybindingsManager())
    app.statusBar = tui.NewText("")
    app.loader = tui.NewLoader("Thinking...")

    app.ui.AddChild(app.messageList)
    app.ui.AddChild(app.statusBar)
    app.ui.AddChild(app.editor)
    app.ui.SetFocus(app.editor)

    // Build agent: pass app as QuestionHandler for ask_user tool
    tools := tools.CodingTools(app.cwd, app)
    app.agent = agent.New(agent.AgentConfig{
        ToolExecution: agent.ToolExecParallel,
        GetAPIKey:     func(_ ai.Provider) string { return app.apiKey },
    })
    app.agent.State().Model = app.model
    app.agent.State().SystemPrompt = prompt.BuildSystemPrompt(app.cwd, tools)
    app.agent.State().Tools = tools

    // Subscribe to agent events (messages, tools, liveness)
    app.agent.Subscribe(app.handleAgentEvent)

    // Editor submit handler
    app.editor.OnSubmit = func(text string) {
        if strings.TrimSpace(text) == "" { return }
        app.editor.SetText("")
        go app.runPrompt(text)
    }

    // Input listener for ctrl+c (abort)
    app.ui.AddInputListener(func(data []byte) (bool, []byte) {
        if tui.MatchesKey(data, "ctrl+c") {
            if app.agent.State().IsStreaming {
                app.agent.Abort()
                return true, nil
            }
        }
        return false, nil
    })

    // Update status bar
    app.updateStatusBar()

    app.ui.Start()
    app.ui.RequestRender()

    // Handle initial prompt
    if args.Prompt != "" {
        go app.runPrompt(args.Prompt)
    }
}

func (app *interactiveApp) runPrompt(text string) {
    app.messageList.addUserMessage(text)
    app.ui.RemoveChild(app.editor)
    app.ui.AddChild(app.loader)
    app.ui.RequestRender()

    err := app.agent.Run(text)
    if err != nil {
        app.messageList.addErrorMessage(err.Error())
    }

    app.ui.RemoveChild(app.loader)
    app.ui.AddChild(app.editor)
    app.ui.SetFocus(app.editor)
    app.ui.RequestRender()
}

func (app *interactiveApp) handleAgentEvent(event agent.AgentEvent, ctx context.Context) {
    switch event.Type {
    case agent.MessageUpdate:
        if event.AssistantMessageEvent != nil {
            switch event.AssistantMessageEvent.Type {
            case ai.EventTextDelta:
                app.messageList.appendAssistantText(event.AssistantMessageEvent.Delta)
                app.ui.RequestRender()
            case ai.EventThinkingDelta:
                // show thinking indicator
            }
        }
    case agent.MessageEnd:
        if am, ok := event.Message.(*ai.AssistantMessage); ok {
            app.messageList.finalizeAssistantMessage(am)
            app.ui.RequestRender()
        }
    case agent.ToolExecutionStart:
        app.messageList.addToolExecution(event.ToolName, event.ToolArgs)
        app.ui.RequestRender()
    case agent.ToolExecutionEnd:
        app.messageList.finalizeToolExecution(event.ToolCallID, event.ToolResult, event.ToolIsError)
        app.ui.RequestRender()
    case agent.LivenessUpdate:
        app.updateLivenessDisplay(event.Liveness)
        app.ui.RequestRender()
    }
    app.updateStatusBar()
}

func (app *interactiveApp) updateStatusBar() {
    model := app.agent.State().Model
    streaming := ""
    if app.agent.State().IsStreaming {
        streaming = " (streaming...)"
    }
    app.statusBar.SetText(fmt.Sprintf(" %s/%s%s", model.Provider, model.ID, streaming))
}

func (app *interactiveApp) updateLivenessDisplay(liveness agent.LivenessEvent) {
    // Update status bar with liveness state:
    //   StatusThinking  -> "Thinking... (3.2s)"
    //   StatusExecuting -> "Running bash... (1.5s)"
    //   StatusStalled   -> "⚠ Stalled: bash (45s)" (highlighted)
    //   StatusIdle      -> model/provider only
    // Compaction states also surface here:
    //   Compacting      -> "Compacting context..."
    //   Overflow        -> "⚠ Context overflow, recovering..."
    model := app.agent.State().Model
    var status string
    switch liveness.Status {
    case agent.StatusThinking:
        status = fmt.Sprintf(" Thinking... (%s)", liveness.Elapsed.Truncate(100*time.Millisecond))
    case agent.StatusExecuting:
        status = fmt.Sprintf(" Running %s... (%s)", liveness.ToolName, liveness.Elapsed.Truncate(100*time.Millisecond))
    case agent.StatusStalled:
        status = fmt.Sprintf(" \x1b[33m⚠ Stalled: %s (%s)\x1b[0m", liveness.ToolName, liveness.Elapsed.Truncate(time.Second))
    case agent.StatusCompacting:
        status = " Compacting context..."
    case agent.StatusOverflowRecovery:
        status = " \x1b[33m⚠ Context overflow, recovering...\x1b[0m"
    default:
        status = ""
    }
    app.statusBar.SetText(fmt.Sprintf(" %s/%s%s", model.Provider, model.ID, status))
}
```

### Message list component

```go
type messageListComponent struct {
    entries []messageEntry
    mu      sync.RWMutex
}

type messageEntry struct {
    role    string // "user", "assistant", "tool", "error"
    text    string
    toolName string
    isError  bool
}

func (m *messageListComponent) Render(width int) []string {
    m.mu.RLock()
    defer m.mu.RUnlock()

    var lines []string
    for _, entry := range m.entries {
        switch entry.role {
        case "user":
            lines = append(lines, "")
            lines = append(lines, fmt.Sprintf("\x1b[1;36m> %s\x1b[0m", entry.text))
        case "assistant":
            md := tui.NewMarkdown(entry.text)
            lines = append(lines, md.Render(width)...)
        case "tool":
            header := fmt.Sprintf("\x1b[33m[%s]\x1b[0m", entry.toolName)
            lines = append(lines, header)
            // Show truncated tool output
        case "error":
            lines = append(lines, fmt.Sprintf("\x1b[31mError: %s\x1b[0m", entry.text))
        }
    }
    return lines
}

func (m *messageListComponent) addUserMessage(text string)
func (m *messageListComponent) appendAssistantText(delta string)
func (m *messageListComponent) finalizeAssistantMessage(msg *ai.AssistantMessage)
func (m *messageListComponent) addToolExecution(name string, args map[string]any)
func (m *messageListComponent) finalizeToolExecution(id string, result *agent.AgentToolResult, isError bool)
func (m *messageListComponent) addErrorMessage(text string)
func (m *messageListComponent) Invalidate()
```

## 7.4 Tests

Integration-level tests using VirtualTerminal:

- Start interactive mode with faux provider, verify initial render
- Submit prompt, verify agent runs, response displayed
- Abort with ctrl+c, verify agent stops
- Print mode: verify stdout output matches expected
- Liveness events: verify status bar updates for thinking/executing/stalled states

### Verification criteria

- [ ] `go build ./cmd/gcode` produces a working binary
- [ ] Print mode: `echo "hello" | gcode -p "respond" --model faux` outputs text to stdout
- [ ] Interactive mode: launches TUI, accepts input, shows responses
- [ ] Ctrl+C during streaming aborts the agent
- [ ] Model resolution works for "provider/model" and bare "model" formats
- [ ] Liveness state (thinking/executing/stalled) shows in status bar


# Phase 8: Integration and polish

System prompt construction, settings, session persistence, and final wiring.

## 8.1 System prompt builder (`internal/prompt/builder.go`)

```go
func BuildSystemPrompt(cwd string, tools []agent.AgentTool) string
```

Assemble from:
1. Role declaration: "You are an expert coding assistant."
2. Available tools: list each tool's description and usage guidelines
3. Guidelines based on active tools:
   - "Use bash for file operations like ls, grep, find"
   - "Use edit for precise changes (oldText must match exactly)"
   - "Use write only for new files or complete rewrites"
   - "Be concise in your responses"
   - "Show file paths clearly when working with files"
4. Project context: if `AGENTS.md` or `.pi/agent/AGENTS.md` exists in cwd, include it
5. Current date and working directory

## 8.2 Settings (`internal/config/settings.go`)

```go
type Settings struct {
    DefaultModel    string `json:"defaultModel,omitempty"`
    DefaultProvider string `json:"defaultProvider,omitempty"`
    ThinkingLevel   string `json:"thinkingLevel,omitempty"`
    CustomModels    []ai.Model `json:"customModels,omitempty"`
}

// LoadSettings reads from ~/.gcode/settings.json (global)
// and .gcode/settings.json (project-local). Project overrides global.
func LoadSettings() Settings

func SaveSettings(s Settings) error
```

## 8.3 Auth storage (`internal/config/auth.go`)

```go
type AuthStorage struct {
    Keys map[string]string `json:"keys"` // provider -> api key
}

// LoadAuth reads from ~/.gcode/auth.json
func LoadAuth() AuthStorage

func SaveAuth(a AuthStorage) error

// GetAPIKey returns the key for a provider from storage or environment.
func (a AuthStorage) GetAPIKey(provider ai.Provider) string {
    if key, ok := a.Keys[string(provider)]; ok {
        return key
    }
    // Fall back to env vars
    envMap := map[ai.Provider]string{
        ai.ProviderAnthropic: "ANTHROPIC_API_KEY",
        ai.ProviderOpenAI:    "OPENAI_API_KEY",
        ai.ProviderGoogle:    "GOOGLE_API_KEY",
        ai.ProviderGroq:      "GROQ_API_KEY",
        ai.ProviderXAI:       "XAI_API_KEY",
    }
    if envVar, ok := envMap[provider]; ok {
        return os.Getenv(envVar)
    }
    return ""
}
```

## 8.4 Session integration

Wire SQLite session persistence into the interactive app. Sessions are stored in `~/.gcode/gcode.db` (global) and `.gcode/project.db` (per project).

```go
func (app *interactiveApp) initSession() {
    // Open project-local database (or global if no project context)
    dbPath := filepath.Join(app.cwd, ".gcode", "project.db")
    os.MkdirAll(filepath.Dir(dbPath), 0755)

    db, err := store.Open(dbPath)
    if err != nil {
        // Log warning and continue without persistence
        return
    }
    app.db = db

    // Create a new session
    sess, err := db.CreateSession(app.cwd)
    if err != nil {
        return
    }
    app.sessionID = sess.ID
}

func (app *interactiveApp) persistMessage(msg agent.AgentMessage) {
    if app.db == nil {
        return
    }
    entry, err := app.db.AppendEntry(app.sessionID, app.leafID, store.EntryTypeMessage, msg)
    if err != nil {
        return
    }
    app.leafID = entry.ID
}
```

## 8.5 Compaction integration

Wire compaction into the agent's `transformContext` hook. Uses `pkg/store` for branch retrieval and entry persistence.

```go
func (app *interactiveApp) setupCompaction() {
    app.agent.Config().TransformContext = func(ctx context.Context, messages []agent.AgentMessage) ([]agent.AgentMessage, error) {
        tokenCount := compaction.EstimateContextTokens(messages)
        if !compaction.ShouldCompact(tokenCount, app.model.ContextWindow, compaction.DefaultCompactionSettings) {
            return messages, nil
        }

        if app.db == nil {
            return messages, nil // no persistence, can't compact
        }

        // Get the current branch from the store
        entries, err := app.db.GetBranch(app.leafID)
        if err != nil {
            return messages, nil // fail open
        }

        // Update liveness: compacting
        app.updateLivenessDisplay(agent.LivenessEvent{Status: agent.StatusCompacting})
        app.ui.RequestRender()

        // Run compaction
        result, err := compaction.Compact(ctx, entries, messages, app.model, app.apiKey, compaction.DefaultCompactionSettings, "", nil)
        if err != nil {
            return messages, nil // fail open
        }

        // Persist compaction entry
        compEntry, err := app.db.AppendEntry(app.sessionID, app.leafID, store.EntryTypeCompaction, store.CompactionData{
            Summary:          result.Summary,
            FirstKeptEntryID: result.FirstKeptEntryID,
            TokensBefore:     result.TokensBefore,
        })
        if err == nil {
            app.leafID = compEntry.ID
        }

        // Rebuild context from the updated branch
        updatedEntries, err := app.db.GetBranch(app.leafID)
        if err != nil {
            return messages, nil
        }
        sessCtx, err := store.BuildContext(updatedEntries)
        if err != nil {
            return messages, nil
        }
        return sessCtx.Messages, nil
    }
}
```

## 8.6 Plugin system integration (`pkg/plugin`)

Skills and subprocess plugins are loaded at startup and registered as additional tools or system prompt fragments.

### Skills

Markdown skill files are loaded from two directories:
- `~/.gcode/skills/` (global, user-installed)
- `.gcode/skills/` (project-local)

Each skill directory contains a `SKILL.md` with YAML frontmatter (name, description, trigger patterns). The skill loader parses these and injects them into the system prompt as available skills, following the same format as pi's skill injection.

```go
func LoadSkills(globalDir, projectDir string) ([]plugin.Skill, error)

// Skills are injected into the system prompt, not registered as tools.
// The system prompt builder accepts a skill list:
func BuildSystemPrompt(cwd string, tools []agent.AgentTool, skills []plugin.Skill) string
```

### Subprocess/RPC plugins

Plugin executables live in `~/.gcode/plugins/`. Each plugin is a subprocess that communicates over stdin/stdout using a JSON-RPC protocol. Plugins register tools that the agent can call.

```go
func LoadPlugins(pluginDir string) ([]plugin.Plugin, error)

// Each plugin exposes one or more tools:
func (p *Plugin) Tools() []agent.AgentTool
```

Plugin lifecycle:
1. On startup, scan `~/.gcode/plugins/` for executables.
2. Launch each plugin subprocess, handshake over JSON-RPC to discover its tools.
3. Register discovered tools with the agent.
4. On shutdown, send terminate signal and wait for graceful exit.

## 8.7 Final verification checklist

- [ ] `go build ./cmd/gcode` produces a single binary
- [ ] Binary size is reasonable (< 30MB without debug symbols)
- [ ] `gcode -p "What is 2+2?" -m anthropic/claude-haiku-4-20250514` returns a response
- [ ] Interactive mode launches, accepts input, shows streaming responses
- [ ] All 6 tools work: read, bash, edit, write, ask_user, fetch
- [ ] `ask_user` renders interactive prompts in TUI mode, returns error in pipe mode
- [ ] Session is persisted to SQLite (`~/.gcode/gcode.db` or `.gcode/project.db`)
- [ ] Compaction triggers on long conversations, status shown in TUI
- [ ] Liveness state (thinking/executing/stalled) displays in status bar
- [ ] Compaction state (compacting/overflow recovery) displays in status bar
- [ ] Ctrl+C aborts mid-stream
- [ ] All 3 providers work (Anthropic, OpenAI, Google)
- [ ] `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GOOGLE_API_KEY` env vars resolve correctly
- [ ] Skills loaded from `~/.gcode/skills/` and `.gcode/skills/`
- [ ] Subprocess plugins loaded from `~/.gcode/plugins/`
- [ ] Error handling: invalid API key, network error, unknown model
- [ ] No goroutine leaks (check with `runtime.NumGoroutine()` in tests)

## 8.8 Future work (not in initial scope)

These are explicitly deferred:

- **RPC mode**: JSON-RPC over stdio for IDE integration
- **OAuth**: Provider-specific OAuth flows
- **Image paste**: Terminal image protocol detection and paste handling
- **Model selector overlay**: Interactive model picker in TUI
- **Session tree navigation**: UI for branching and navigating between branches
- **Themes**: Configurable color schemes
- **Web UI**: Separate project, not part of the Go agent
