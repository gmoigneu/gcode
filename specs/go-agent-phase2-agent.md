# Phase 2: Agent runtime (`pkg/agent`)

> **Project:** gcode. Requires Go 1.24+. Interfaces are designed with future subagent support in mind, but only the single-loop agent is implemented initially.

The stateful agent loop. Manages conversation state, tool execution, event streaming, and message queues. Depends only on `pkg/ai`.

## 2.1 Core types (`pkg/agent/types.go`)

### AgentMessage interface

```go
// AgentMessage is the union of LLM messages + custom app messages.
// In Go, we use an interface instead of TypeScript's declaration merging.
type AgentMessage interface {
    MessageRole() string
    MessageTimestamp() int64
}

// ai.UserMessage, ai.AssistantMessage, ai.ToolResultMessage all implement this.
// Apps add custom types by implementing the interface.
```

### AgentTool

```go
type AgentToolResult struct {
    Content []ai.Content
    Details any
}

type AgentToolUpdateFunc func(partial AgentToolResult)

type AgentTool struct {
    ai.Tool // embedded: Name, Description, Parameters
    Label   string

    // Execute runs the tool. Signal is the parent context for cancellation.
    // onUpdate is called with partial results during long-running execution.
    Execute func(toolCallID string, params map[string]any, signal context.Context, onUpdate AgentToolUpdateFunc) (AgentToolResult, error)

    // PrepareArguments optionally transforms arguments before validation.
    PrepareArguments func(args map[string]any) map[string]any
}
```

### AgentState

```go
type AgentState struct {
    SystemPrompt     string
    Model            ai.Model
    ThinkingLevel    ai.ThinkingLevel
    Tools            []AgentTool    // defensive copy on set
    Messages         []AgentMessage // defensive copy on set
    IsStreaming       bool
    StreamingMessage AgentMessage   // nil when not streaming
    PendingToolCalls map[string]bool // set of active tool call IDs
    ErrorMessage     string
}

func NewAgentState() *AgentState {
    return &AgentState{
        PendingToolCalls: make(map[string]bool),
    }
}

// Clone returns a snapshot copy (shallow copy of slices).
func (s *AgentState) Clone() *AgentState
```

### AgentStatus and LivenessEvent

**Important: promoted from backlog.** The agent loop must emit structured liveness events so the TUI knows what state the agent is in.

```go
type AgentStatus int

const (
    StatusIdle      AgentStatus = iota
    StatusThinking              // LLM inference in flight
    StatusExecuting             // Tool call running
    StatusStalled               // Exceeded duration threshold
)

type LivenessEvent struct {
    Status   AgentStatus
    ToolName string        // Which tool (when StatusExecuting)
    Elapsed  time.Duration // Time in current state
}
```

### AgentEvent

```go
type AgentEventType string

const (
    AgentEventStart          AgentEventType = "agent_start"
    AgentEventEnd            AgentEventType = "agent_end"
    TurnStart                AgentEventType = "turn_start"
    TurnEnd                  AgentEventType = "turn_end"
    MessageStart             AgentEventType = "message_start"
    MessageUpdate            AgentEventType = "message_update"
    MessageEnd               AgentEventType = "message_end"
    ToolExecutionStart       AgentEventType = "tool_execution_start"
    ToolExecutionUpdate      AgentEventType = "tool_execution_update"
    ToolExecutionEnd         AgentEventType = "tool_execution_end"
    LivenessUpdate           AgentEventType = "liveness_update"
)

type AgentEvent struct {
    Type AgentEventType

    // agent_end
    NewMessages []AgentMessage

    // message_start, message_update, message_end
    Message AgentMessage

    // message_update: the underlying LLM event
    AssistantMessageEvent *ai.AssistantMessageEvent

    // turn_end
    TurnMessage AgentMessage
    ToolResults []ai.ToolResultMessage

    // tool_execution_start, tool_execution_update, tool_execution_end
    ToolCallID   string
    ToolName     string
    ToolArgs     map[string]any
    ToolResult   *AgentToolResult
    ToolIsError  bool

    // liveness_update
    Liveness *LivenessEvent
}
```

### Hooks

```go
type BeforeToolCallResult struct {
    Block  bool
    Reason string
}

type BeforeToolCallContext struct {
    AssistantMessage *ai.AssistantMessage
    ToolCall         ai.ToolCall
    Args             map[string]any
    Context          *AgentState
}

type AfterToolCallContext struct {
    BeforeToolCallContext
    Result  AgentToolResult
    IsError bool
}

type AfterToolCallResult struct {
    Content []ai.Content // nil = no override
    Details any          // nil = no override
    IsError *bool        // nil = no override
}

type ConvertToLLMFunc func(messages []AgentMessage) []ai.Message
type TransformContextFunc func(ctx context.Context, messages []AgentMessage) ([]AgentMessage, error)
type GetAPIKeyFunc func(provider ai.Provider) string
type BeforeToolCallFunc func(ctx BeforeToolCallContext) BeforeToolCallResult
type AfterToolCallFunc func(ctx AfterToolCallContext) AfterToolCallResult
```

### AgentConfig

```go
type ToolExecutionMode string

const (
    ToolExecParallel   ToolExecutionMode = "parallel"
    ToolExecSequential ToolExecutionMode = "sequential"
)

type QueueMode string

const (
    QueueAll        QueueMode = "all"
    QueueOneAtATime QueueMode = "one-at-a-time"
)

type AgentConfig struct {
    ConvertToLLM     ConvertToLLMFunc
    TransformContext TransformContextFunc
    GetAPIKey        GetAPIKeyFunc
    BeforeToolCall   BeforeToolCallFunc
    AfterToolCall    AfterToolCallFunc
    ToolExecution    ToolExecutionMode  // default: parallel
    SteeringMode     QueueMode          // default: one-at-a-time
    FollowUpMode     QueueMode          // default: one-at-a-time
    StreamFn         ai.SimpleStreamFunc // default: ai.StreamSimple
    ThinkingBudgets  *ai.ThinkingBudgets

    // Liveness stall thresholds
    ThinkingStallTimeout time.Duration // default: 60s
    ToolStallTimeout     time.Duration // default: 120s
}
```

## 2.2 Message queue (`pkg/agent/queue.go`)

```go
type PendingMessageQueue struct {
    mu       sync.Mutex
    messages []AgentMessage
    mode     QueueMode
}

func NewPendingMessageQueue(mode QueueMode) *PendingMessageQueue

func (q *PendingMessageQueue) Enqueue(msg AgentMessage) {
    q.mu.Lock()
    defer q.mu.Unlock()
    q.messages = append(q.messages, msg)
}

func (q *PendingMessageQueue) Drain() []AgentMessage {
    q.mu.Lock()
    defer q.mu.Unlock()
    if len(q.messages) == 0 {
        return nil
    }
    switch q.mode {
    case QueueAll:
        msgs := q.messages
        q.messages = nil
        return msgs
    case QueueOneAtATime:
        msg := q.messages[0]
        q.messages = q.messages[1:]
        return []AgentMessage{msg}
    }
    return nil
}

func (q *PendingMessageQueue) HasItems() bool
func (q *PendingMessageQueue) Clear()
```

## 2.3 Agent struct (`pkg/agent/agent.go`)

```go
type Agent struct {
    state          *AgentState
    config         AgentConfig
    steeringQueue  *PendingMessageQueue
    followUpQueue  *PendingMessageQueue
    listeners      []AgentEventListener
    listenersMu    sync.RWMutex

    // Active run tracking
    activeCancel context.CancelFunc
    activeDone   chan struct{} // closed when run completes
    activeMu     sync.Mutex
}

type AgentEventListener func(event AgentEvent, signal context.Context)

func New(config AgentConfig) *Agent {
    return &Agent{
        state:         NewAgentState(),
        config:        config,
        steeringQueue: NewPendingMessageQueue(config.SteeringMode),
        followUpQueue: NewPendingMessageQueue(config.FollowUpMode),
    }
}

// State returns a snapshot of the current state.
func (a *Agent) State() *AgentState {
    return a.state.Clone()
}

// Subscribe registers an event listener. Returns an unsubscribe function.
func (a *Agent) Subscribe(listener AgentEventListener) func() {
    a.listenersMu.Lock()
    defer a.listenersMu.Unlock()
    a.listeners = append(a.listeners, listener)
    idx := len(a.listeners) - 1
    return func() {
        a.listenersMu.Lock()
        defer a.listenersMu.Unlock()
        a.listeners = slices.Delete(a.listeners, idx, idx+1)
    }
}

// Run starts the agent with user input. Blocks until complete.
// Returns error if a run is already active.
func (a *Agent) Run(input string, images ...ai.ImageContent) error {
    return a.RunMessages(buildUserMessages(input, images))
}

// RunMessages starts the agent with pre-built messages.
func (a *Agent) RunMessages(messages []AgentMessage) error {
    a.activeMu.Lock()
    if a.activeDone != nil {
        select {
        case <-a.activeDone:
            // previous run finished, ok to start new one
        default:
            a.activeMu.Unlock()
            return fmt.Errorf("agent is already running")
        }
    }

    ctx, cancel := context.WithCancel(context.Background())
    done := make(chan struct{})
    a.activeCancel = cancel
    a.activeDone = done
    a.activeMu.Unlock()

    defer func() {
        cancel()
        close(done)
    }()

    a.state.IsStreaming = true
    a.state.StreamingMessage = nil
    a.state.ErrorMessage = ""

    err := runLoop(ctx, a, messages)

    a.state.IsStreaming = false
    a.state.StreamingMessage = nil
    return err
}

// Continue resumes from the current transcript.
func (a *Agent) Continue() error {
    // Validate: messages not empty, last message not assistant (unless queues have items)
    return a.RunMessages(nil)
}

// Steer injects a message into the steering queue.
func (a *Agent) Steer(msg AgentMessage) {
    a.steeringQueue.Enqueue(msg)
}

// FollowUp enqueues a message for post-processing.
func (a *Agent) FollowUp(msg AgentMessage) {
    a.followUpQueue.Enqueue(msg)
}

// Abort cancels the current run.
func (a *Agent) Abort() {
    a.activeMu.Lock()
    defer a.activeMu.Unlock()
    if a.activeCancel != nil {
        a.activeCancel()
    }
}

// WaitForIdle blocks until the current run completes.
func (a *Agent) WaitForIdle() {
    a.activeMu.Lock()
    done := a.activeDone
    a.activeMu.Unlock()
    if done != nil {
        <-done
    }
}

// Signal returns the current run's context (for checking cancellation).
func (a *Agent) Signal() context.Context {
    // return active context or context.Background()
}

// Reset clears all state.
func (a *Agent) Reset() {
    a.state = NewAgentState()
    a.steeringQueue.Clear()
    a.followUpQueue.Clear()
}

// emit sends an event to all listeners, updating state first.
func (a *Agent) emit(event AgentEvent, ctx context.Context) {
    // Update state based on event type (state reducer)
    a.processEvent(event)

    a.listenersMu.RLock()
    listeners := slices.Clone(a.listeners)
    a.listenersMu.RUnlock()

    for _, l := range listeners {
        l(event, ctx)
    }
}

func (a *Agent) processEvent(event AgentEvent) {
    switch event.Type {
    case MessageStart:
        a.state.StreamingMessage = event.Message
    case MessageUpdate:
        a.state.StreamingMessage = event.Message
    case MessageEnd:
        a.state.StreamingMessage = nil
        a.state.Messages = append(a.state.Messages, event.Message)
    case ToolExecutionStart:
        a.state.PendingToolCalls[event.ToolCallID] = true
    case ToolExecutionEnd:
        delete(a.state.PendingToolCalls, event.ToolCallID)
    case TurnEnd:
        if event.TurnMessage != nil {
            if am, ok := event.TurnMessage.(*ai.AssistantMessage); ok {
                if am.StopReason == ai.StopReasonError {
                    a.state.ErrorMessage = am.ErrorMessage
                }
            }
        }
    case AgentEventEnd:
        a.state.StreamingMessage = nil
    }
}
```

## 2.4 Agent loop (`pkg/agent/loop.go`)

```go
func runLoop(ctx context.Context, agent *Agent, initialMessages []AgentMessage) error {
    var newMessages []AgentMessage

    // Emit agent_start
    agent.emit(AgentEvent{Type: AgentEventStart}, ctx)

    // Add initial messages to state and emit them
    if len(initialMessages) > 0 {
        agent.emit(AgentEvent{Type: TurnStart}, ctx)
        for _, msg := range initialMessages {
            agent.state.Messages = append(agent.state.Messages, msg)
            agent.emit(AgentEvent{Type: MessageStart, Message: msg}, ctx)
            agent.emit(AgentEvent{Type: MessageEnd, Message: msg}, ctx)
            newMessages = append(newMessages, msg)
        }
    }

    pendingMessages := []AgentMessage(nil)
    firstTurn := true

    // Outer loop: follow-ups
    for {
        // Inner loop: tool calls + steering
        for {
            if ctx.Err() != nil {
                agent.emit(AgentEvent{Type: AgentEventEnd, NewMessages: newMessages}, ctx)
                return ctx.Err()
            }

            if !firstTurn || len(pendingMessages) > 0 {
                if !firstTurn {
                    agent.emit(AgentEvent{Type: TurnStart}, ctx)
                }
                // Emit pending messages
                for _, msg := range pendingMessages {
                    agent.state.Messages = append(agent.state.Messages, msg)
                    agent.emit(AgentEvent{Type: MessageStart, Message: msg}, ctx)
                    agent.emit(AgentEvent{Type: MessageEnd, Message: msg}, ctx)
                    newMessages = append(newMessages, msg)
                }
                pendingMessages = nil
            }
            firstTurn = false

            // Stream assistant response
            assistantMsg, err := streamAssistantResponse(ctx, agent)
            if err != nil || assistantMsg.StopReason == ai.StopReasonError || assistantMsg.StopReason == ai.StopReasonAborted {
                agent.emit(AgentEvent{Type: TurnEnd, TurnMessage: assistantMsg}, ctx)
                agent.emit(AgentEvent{Type: AgentEventEnd, NewMessages: newMessages}, ctx)
                return err
            }
            newMessages = append(newMessages, assistantMsg)

            // Extract tool calls
            toolCalls := extractToolCalls(assistantMsg)

            var toolResults []ai.ToolResultMessage
            if len(toolCalls) > 0 {
                toolResults = executeToolCalls(ctx, agent, assistantMsg, toolCalls)
                for _, tr := range toolResults {
                    newMessages = append(newMessages, &tr)
                }
            }

            agent.emit(AgentEvent{Type: TurnEnd, TurnMessage: assistantMsg, ToolResults: toolResults}, ctx)

            // Check steering queue
            steering := agent.steeringQueue.Drain()
            if len(steering) > 0 {
                pendingMessages = steering
                continue
            }

            // No tool calls = done with inner loop
            if len(toolCalls) == 0 {
                break
            }
        }

        // Check follow-up queue
        followUps := agent.followUpQueue.Drain()
        if len(followUps) == 0 {
            break
        }
        pendingMessages = followUps
        firstTurn = false
    }

    agent.emit(AgentEvent{Type: AgentEventEnd, NewMessages: newMessages}, ctx)
    return nil
}
```

### Stream assistant response

```go
func streamAssistantResponse(ctx context.Context, agent *Agent) (*ai.AssistantMessage, error) {
    // 1. Apply transformContext if configured
    messages := slices.Clone(agent.state.Messages)
    if agent.config.TransformContext != nil {
        var err error
        messages, err = agent.config.TransformContext(ctx, messages)
        if err != nil {
            return nil, err
        }
    }

    // 2. Convert to LLM messages
    var llmMessages []ai.Message
    if agent.config.ConvertToLLM != nil {
        llmMessages = agent.config.ConvertToLLM(messages)
    } else {
        llmMessages = defaultConvertToLLM(messages)
    }

    // 3. Build context
    llmCtx := ai.Context{
        SystemPrompt: agent.state.SystemPrompt,
        Messages:     llmMessages,
        Tools:        extractToolDefs(agent.state.Tools),
    }

    // 4. Resolve API key
    apiKey := ""
    if agent.config.GetAPIKey != nil {
        apiKey = agent.config.GetAPIKey(agent.state.Model.Provider)
    }

    // 5. Stream
    opts := &ai.SimpleStreamOptions{
        StreamOptions: ai.StreamOptions{
            Signal: ctx,
            APIKey: apiKey,
        },
        Reasoning: agent.state.ThinkingLevel,
    }

    streamFn := agent.config.StreamFn
    if streamFn == nil {
        streamFn = ai.StreamSimple
    }
    stream := streamFn(agent.state.Model, llmCtx, opts)

    // 6. Process events
    for event := range stream.C {
        switch event.Type {
        case ai.EventStart:
            agent.emit(AgentEvent{Type: MessageStart, Message: event.Partial}, ctx)
        case ai.EventDone:
            agent.emit(AgentEvent{Type: MessageEnd, Message: event.Message}, ctx)
        case ai.EventError:
            agent.emit(AgentEvent{Type: MessageEnd, Message: event.Error}, ctx)
        default:
            // text_delta, thinking_delta, toolcall_delta, etc.
            agent.emit(AgentEvent{Type: MessageUpdate, Message: event.Partial, AssistantMessageEvent: &event}, ctx)
        }
    }

    return stream.Result(), nil // Note: Result() returns value, not pointer. Adapt.
}
```

### Tool execution

```go
func executeToolCalls(ctx context.Context, agent *Agent, assistantMsg *ai.AssistantMessage, toolCalls []ai.ToolCall) []ai.ToolResultMessage {
    switch agent.config.ToolExecution {
    case ToolExecSequential:
        return executeToolCallsSequential(ctx, agent, assistantMsg, toolCalls)
    default:
        return executeToolCallsParallel(ctx, agent, assistantMsg, toolCalls)
    }
}

func executeToolCallsParallel(ctx context.Context, agent *Agent, assistantMsg *ai.AssistantMessage, toolCalls []ai.ToolCall) []ai.ToolResultMessage {
    // Phase 1: Sequential preparation
    type prepared struct {
        tool     *AgentTool
        toolCall ai.ToolCall
        args     map[string]any
        blocked  bool
        reason   string
    }
    preps := make([]prepared, len(toolCalls))

    for i, tc := range toolCalls {
        agent.emit(AgentEvent{Type: ToolExecutionStart, ToolCallID: tc.ID, ToolName: tc.Name, ToolArgs: tc.Arguments}, ctx)

        tool := findTool(agent.state.Tools, tc.Name)
        if tool == nil {
            preps[i] = prepared{blocked: true, reason: fmt.Sprintf("unknown tool: %s", tc.Name)}
            continue
        }

        args := tc.Arguments
        if tool.PrepareArguments != nil {
            args = tool.PrepareArguments(args)
        }

        if agent.config.BeforeToolCall != nil {
            result := agent.config.BeforeToolCall(BeforeToolCallContext{
                AssistantMessage: assistantMsg,
                ToolCall:         tc,
                Args:             args,
            })
            if result.Block {
                preps[i] = prepared{blocked: true, reason: result.Reason}
                continue
            }
        }

        preps[i] = prepared{tool: tool, toolCall: tc, args: args}
    }

    // Phase 2: Parallel execution
    type result struct {
        index  int
        result AgentToolResult
        err    error
    }

    resultCh := make(chan result, len(preps))
    var wg sync.WaitGroup

    for i, p := range preps {
        if p.blocked || p.tool == nil {
            continue
        }
        wg.Add(1)
        go func(idx int, prep prepared) {
            defer wg.Done()
            onUpdate := func(partial AgentToolResult) {
                agent.emit(AgentEvent{
                    Type: ToolExecutionUpdate, ToolCallID: prep.toolCall.ID,
                    ToolName: prep.toolCall.Name, ToolArgs: prep.args, ToolResult: &partial,
                }, ctx)
            }
            res, err := prep.tool.Execute(prep.toolCall.ID, prep.args, ctx, onUpdate)
            resultCh <- result{index: idx, result: res, err: err}
        }(i, p)
    }

    go func() { wg.Wait(); close(resultCh) }()

    // Collect results
    results := make(map[int]result)
    for r := range resultCh {
        results[r.index] = r
    }

    // Phase 3: Finalize in order
    var toolResults []ai.ToolResultMessage
    for i, p := range preps {
        var tr ai.ToolResultMessage
        tr.Role = "toolResult"
        tr.ToolCallID = p.toolCall.ID
        tr.ToolName = p.toolCall.Name
        tr.Timestamp = time.Now().UnixMilli()

        if p.blocked {
            tr.IsError = true
            tr.Content = []ai.Content{ai.TextContent{Type: "text", Text: p.reason}}
        } else if r, ok := results[i]; ok {
            if r.err != nil {
                tr.IsError = true
                tr.Content = []ai.Content{ai.TextContent{Type: "text", Text: r.err.Error()}}
            } else {
                tr.Content = r.result.Content
                tr.Details = r.result.Details
            }
        }

        // afterToolCall hook
        if agent.config.AfterToolCall != nil && !p.blocked {
            override := agent.config.AfterToolCall(AfterToolCallContext{
                BeforeToolCallContext: BeforeToolCallContext{
                    AssistantMessage: assistantMsg, ToolCall: p.toolCall, Args: p.args,
                },
                Result:  AgentToolResult{Content: tr.Content, Details: tr.Details},
                IsError: tr.IsError,
            })
            if override.Content != nil { tr.Content = override.Content }
            if override.Details != nil { tr.Details = override.Details }
            if override.IsError != nil { tr.IsError = *override.IsError }
        }

        agent.emit(AgentEvent{Type: ToolExecutionEnd, ToolCallID: tr.ToolCallID, ToolName: tr.ToolName, ToolResult: &AgentToolResult{Content: tr.Content, Details: tr.Details}, ToolIsError: tr.IsError}, ctx)
        agent.emit(AgentEvent{Type: MessageStart, Message: &tr}, ctx)
        agent.emit(AgentEvent{Type: MessageEnd, Message: &tr}, ctx)
        agent.state.Messages = append(agent.state.Messages, &tr)
        toolResults = append(toolResults, tr)
    }

    return toolResults
}
```

### Default ConvertToLLM

```go
func defaultConvertToLLM(messages []AgentMessage) []ai.Message {
    var result []ai.Message
    for _, msg := range messages {
        switch m := msg.(type) {
        case *ai.UserMessage:
            result = append(result, m)
        case *ai.AssistantMessage:
            if m.StopReason != ai.StopReasonError && m.StopReason != ai.StopReasonAborted {
                result = append(result, m)
            }
        case *ai.ToolResultMessage:
            result = append(result, m)
        }
        // Custom message types are silently dropped by default.
        // Apps override ConvertToLLM to handle them.
    }
    return result
}
```

## 2.5 Liveness events (`pkg/agent/liveness.go`)

The agent loop emits `LivenessUpdate` events so the TUI can display the current agent state (thinking, executing a tool, idle, or stalled).

### Behavior

The agent loop integrates liveness as follows:

1. **Emit `StatusThinking`** when starting an LLM stream (at the top of `streamAssistantResponse`).
2. **Emit `StatusExecuting`** with the tool name when a tool begins execution (at `ToolExecutionStart`).
3. **Emit `StatusIdle`** when the loop ends (at `AgentEventEnd`).
4. **Background ticker goroutine:** on entry to each state (thinking or executing), start a goroutine that emits a `LivenessUpdate` every 1s with the current elapsed time. Cancel the goroutine when the state changes.
5. **Stall escalation:** if the elapsed time in a single state exceeds the configured threshold (`ThinkingStallTimeout` for thinking, `ToolStallTimeout` for tool execution), the ticker goroutine escalates the status to `StatusStalled` in subsequent `LivenessUpdate` events. The stall status is purely informational; it does not cancel or abort the operation.

### Liveness tracker

```go
type livenessTracker struct {
    agent     *Agent
    mu        sync.Mutex
    status    AgentStatus
    toolName  string
    stateStart time.Time
    cancel    context.CancelFunc
}

func newLivenessTracker(agent *Agent) *livenessTracker

// SetStatus transitions to a new state and starts the background ticker.
// Cancels any previous ticker.
func (lt *livenessTracker) SetStatus(status AgentStatus, toolName string)

// Stop cancels the background ticker and emits a final StatusIdle event.
func (lt *livenessTracker) Stop(ctx context.Context)

// tick runs in a goroutine, emitting LivenessUpdate every 1s.
// Escalates to StatusStalled when the threshold is exceeded.
func (lt *livenessTracker) tick(ctx context.Context, threshold time.Duration)
```

The `livenessTracker` is created at the start of `runLoop` and stopped at the end. State transitions call `SetStatus`, which cancels the previous ticker goroutine and starts a new one.

Default thresholds when not configured:

```go
func resolveThinkingStallTimeout(cfg AgentConfig) time.Duration {
    if cfg.ThinkingStallTimeout > 0 {
        return cfg.ThinkingStallTimeout
    }
    return 60 * time.Second
}

func resolveToolStallTimeout(cfg AgentConfig) time.Duration {
    if cfg.ToolStallTimeout > 0 {
        return cfg.ToolStallTimeout
    }
    return 120 * time.Second
}
```

## 2.6 Tests

### Unit tests

- `queue_test.go`: Enqueue/drain in both modes, thread safety, clear
- `agent_test.go`: Run with faux provider, verify event sequence (agent_start, turn_start, message_start, ..., agent_end)
- `loop_test.go`:
  - Single turn (text response, no tools): verify events
  - Tool call turn: verify tool_execution_start/end events, tool result appended
  - Multi-turn tool use: tool call -> tool result -> another LLM call -> stop
  - Steering: inject message mid-run, verify it becomes next turn's input
  - Follow-up: verify follow-up processed after tool calls complete
  - Parallel tool execution: two tools called simultaneously, results in source order
  - Abort: cancel context, verify aborted event
  - Error handling: LLM returns error, verify error event and loop stops
- `liveness_test.go`:
  - Thinking/executing/idle transitions: run a single-turn agent, collect `LivenessUpdate` events, verify the status sequence is `StatusThinking` -> `StatusIdle`
  - Thinking then tool then idle: run an agent with a tool call, verify `StatusThinking` -> `StatusExecuting` (with correct tool name) -> `StatusThinking` -> `StatusIdle`
  - Elapsed time increases: collect multiple `LivenessUpdate` events within a single state and verify `Elapsed` is monotonically increasing
  - Stall escalation (thinking): set `ThinkingStallTimeout` to a short duration (e.g. 50ms), use a slow mock LLM stream, verify that `LivenessUpdate` events escalate from `StatusThinking` to `StatusStalled`
  - Stall escalation (tool): set `ToolStallTimeout` to a short duration, use a slow mock tool, verify escalation from `StatusExecuting` to `StatusStalled`
  - Stall does not abort: verify the operation completes successfully even after stall escalation
  - Ticker stops on state change: transition from thinking to executing, verify no further `StatusThinking` liveness events after the transition

### Verification criteria

- [ ] `go test ./pkg/agent/...` passes (including liveness tests)
- [ ] Liveness events reflect correct status transitions throughout the agent loop
- [ ] Stall escalation fires after the configured threshold and does not abort the operation
- [ ] Background ticker goroutines are cleaned up (no goroutine leaks after agent run completes)

### Previous verification criteria (retained)

- [ ] `go test ./pkg/agent/...` passes
- [ ] Agent loop correctly cycles: LLM call -> tool execution -> LLM call -> stop
- [ ] Steering messages are processed before follow-up messages
- [ ] Parallel tool execution runs tools concurrently but emits results in order
- [ ] Context cancellation stops the loop and emits agent_end
- [ ] ConvertToLLM filters out error/aborted assistant messages
- [ ] State updates correctly for each event type
