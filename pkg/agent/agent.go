// Package agent implements the stateful agent loop with tool dispatch and
// liveness events.
package agent

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gmoigneu/gcode/pkg/ai"
)

// AgentEventListener is the callback registered via Subscribe.
type AgentEventListener func(event AgentEvent, signal context.Context)

// runLoopFn is the function invoked by Run / RunMessages to drive the
// conversation. It is a package-level variable so that #19 can install the
// real implementation; tests can replace it with stubs.
var runLoopFn = defaultRunLoop

// defaultRunLoop is a minimal stub that just emits agent_start / agent_end.
// The production loop is installed by loop.go (#19).
func defaultRunLoop(ctx context.Context, agent *Agent, initial []AgentMessage) error {
	agent.emit(AgentEvent{Type: AgentEventStart}, ctx)
	for _, m := range initial {
		agent.state.Messages = append(agent.state.Messages, m)
		agent.emit(AgentEvent{Type: MessageStart, Message: m}, ctx)
		agent.emit(AgentEvent{Type: MessageEnd, Message: m}, ctx)
	}
	agent.emit(AgentEvent{Type: AgentEventEnd, NewMessages: initial}, ctx)
	return nil
}

// Agent is the stateful agent runtime. All exported methods are safe to
// call from any goroutine.
type Agent struct {
	state   *AgentState
	stateMu sync.RWMutex

	config AgentConfig

	steeringQueue *PendingMessageQueue
	followUpQueue *PendingMessageQueue

	listenersMu    sync.RWMutex
	listeners      []listenerEntry
	nextListenerID atomic.Uint64

	runMu        sync.Mutex
	activeCancel context.CancelFunc
	activeDone   chan struct{}
}

type listenerEntry struct {
	id uint64
	fn AgentEventListener
}

// New constructs an agent with the given config. Zero-value config is valid.
func New(config AgentConfig) *Agent {
	if config.SteeringMode == "" {
		config.SteeringMode = QueueOneAtATime
	}
	if config.FollowUpMode == "" {
		config.FollowUpMode = QueueOneAtATime
	}
	if config.ToolExecution == "" {
		config.ToolExecution = ToolExecParallel
	}
	return &Agent{
		state:         NewAgentState(),
		config:        config,
		steeringQueue: NewPendingMessageQueue(config.SteeringMode),
		followUpQueue: NewPendingMessageQueue(config.FollowUpMode),
	}
}

// Config returns the agent's (post-default) configuration.
func (a *Agent) Config() AgentConfig { return a.config }

// State returns a snapshot of the agent state.
func (a *Agent) State() *AgentState {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()
	return a.state.Clone()
}

// SetSystemPrompt updates the system prompt used for future turns.
func (a *Agent) SetSystemPrompt(prompt string) {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	a.state.SystemPrompt = prompt
}

// SetModel updates the model and thinking level used for future turns.
func (a *Agent) SetModel(model ai.Model, thinking ai.ThinkingLevel) {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	a.state.Model = model
	a.state.ThinkingLevel = thinking
}

// SetTools replaces the tool list used for future turns.
func (a *Agent) SetTools(tools []AgentTool) {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	if len(tools) == 0 {
		a.state.Tools = nil
		return
	}
	a.state.Tools = append([]AgentTool(nil), tools...)
}

// Subscribe registers a listener. The returned function removes it.
func (a *Agent) Subscribe(fn AgentEventListener) func() {
	id := a.nextListenerID.Add(1)
	a.listenersMu.Lock()
	a.listeners = append(a.listeners, listenerEntry{id: id, fn: fn})
	a.listenersMu.Unlock()
	return func() {
		a.listenersMu.Lock()
		defer a.listenersMu.Unlock()
		for i, l := range a.listeners {
			if l.id == id {
				a.listeners = append(a.listeners[:i], a.listeners[i+1:]...)
				return
			}
		}
	}
}

// Run starts the agent with a plain user input plus optional images.
// Blocks until the run completes.
func (a *Agent) Run(input string, images ...ai.ImageContent) error {
	return a.RunMessages(buildUserMessages(input, images))
}

// RunMessages starts the agent with pre-built messages. Returns an error
// if another run is in progress.
func (a *Agent) RunMessages(messages []AgentMessage) error {
	if err := a.beginRun(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())

	a.runMu.Lock()
	a.activeCancel = cancel
	a.runMu.Unlock()

	a.stateMu.Lock()
	a.state.IsStreaming = true
	a.state.StreamingMessage = nil
	a.state.ErrorMessage = ""
	a.stateMu.Unlock()

	err := runLoopFn(ctx, a, messages)

	a.stateMu.Lock()
	a.state.IsStreaming = false
	a.state.StreamingMessage = nil
	a.stateMu.Unlock()

	a.endRun(cancel)
	return err
}

// Continue resumes the conversation with no new messages. It relies on the
// queues or the transcript to carry context.
func (a *Agent) Continue() error {
	return a.RunMessages(nil)
}

// Steer enqueues a message in the steering queue. If a run is active, the
// loop picks it up at the next turn boundary.
func (a *Agent) Steer(msg AgentMessage) {
	a.steeringQueue.Enqueue(msg)
}

// FollowUp enqueues a message for post-turn processing.
func (a *Agent) FollowUp(msg AgentMessage) {
	a.followUpQueue.Enqueue(msg)
}

// SteeringQueue and FollowUpQueue expose the underlying queues to the loop.
func (a *Agent) SteeringQueue() *PendingMessageQueue { return a.steeringQueue }
func (a *Agent) FollowUpQueue() *PendingMessageQueue { return a.followUpQueue }

// Abort cancels the active run, if any.
func (a *Agent) Abort() {
	a.runMu.Lock()
	cancel := a.activeCancel
	a.runMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// WaitForIdle blocks until the currently active run finishes. Returns
// immediately if no run is in flight.
func (a *Agent) WaitForIdle() {
	a.runMu.Lock()
	done := a.activeDone
	a.runMu.Unlock()
	if done != nil {
		<-done
	}
}

// Reset clears all agent state and drains the queues. Does not affect the
// listener list or config.
func (a *Agent) Reset() {
	a.stateMu.Lock()
	a.state = NewAgentState()
	a.stateMu.Unlock()
	a.steeringQueue.Clear()
	a.followUpQueue.Clear()
}

// emit updates state via the reducer and fans out to listeners.
func (a *Agent) emit(event AgentEvent, ctx context.Context) {
	a.processEvent(event)

	a.listenersMu.RLock()
	snapshot := make([]AgentEventListener, 0, len(a.listeners))
	for _, l := range a.listeners {
		snapshot = append(snapshot, l.fn)
	}
	a.listenersMu.RUnlock()

	for _, fn := range snapshot {
		fn(event, ctx)
	}
}

// processEvent mutates state based on the event. Holds stateMu internally.
func (a *Agent) processEvent(event AgentEvent) {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()

	switch event.Type {
	case MessageStart:
		a.state.StreamingMessage = event.Message
	case MessageUpdate:
		a.state.StreamingMessage = event.Message
	case MessageEnd:
		a.state.StreamingMessage = nil
		if event.Message != nil {
			a.state.Messages = append(a.state.Messages, event.Message)
		}
	case ToolExecutionStart:
		a.state.PendingToolCalls[event.ToolCallID] = true
	case ToolExecutionEnd:
		delete(a.state.PendingToolCalls, event.ToolCallID)
	case TurnEnd:
		if am, ok := event.TurnMessage.(*ai.AssistantMessage); ok {
			if am != nil && am.StopReason == ai.StopReasonError {
				a.state.ErrorMessage = am.ErrorMessage
			}
		}
	case AgentEventEnd:
		a.state.StreamingMessage = nil
	}
}

// ---- run lifecycle bookkeeping ----

func (a *Agent) beginRun() error {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	if a.activeDone != nil {
		select {
		case <-a.activeDone:
			// previous run finished; acceptable
		default:
			return fmt.Errorf("agent: run already in progress")
		}
	}
	a.activeDone = make(chan struct{})
	return nil
}

func (a *Agent) endRun(cancel context.CancelFunc) {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if a.activeDone != nil {
		close(a.activeDone)
	}
	a.activeCancel = nil
}

// buildUserMessages wraps a text input and any images into a single
// UserMessage.
func buildUserMessages(input string, images []ai.ImageContent) []AgentMessage {
	content := make([]ai.Content, 0, 1+len(images))
	if input != "" {
		content = append(content, &ai.TextContent{Text: input})
	}
	for i := range images {
		img := images[i]
		content = append(content, &img)
	}
	if len(content) == 0 {
		return nil
	}
	return []AgentMessage{&ai.UserMessage{
		Content:   content,
		Timestamp: time.Now().UnixMilli(),
	}}
}

// stateSnapshot returns an unlocked snapshot pointer. Only callers that
// already hold stateMu or are the loop goroutine should use this.
func (a *Agent) stateSnapshot() *AgentState {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()
	return a.state.Clone()
}
