package agent

import (
	"context"
	"time"

	"github.com/gmoigneu/gcode/pkg/ai"
)

// AgentMessage is the union of LLM messages and any custom application
// messages. All ai.Message implementations (UserMessage, AssistantMessage,
// ToolResultMessage) automatically satisfy this interface.
type AgentMessage interface {
	MessageRole() string
	MessageTimestamp() int64
}

// AgentToolResult is what a tool returns to the agent.
type AgentToolResult struct {
	Content []ai.Content
	Details any
}

// AgentToolUpdateFunc is invoked by a long-running tool to report partial
// progress to the agent.
type AgentToolUpdateFunc func(partial AgentToolResult)

// AgentTool is a registered tool: an ai.Tool (schema) plus an executor and
// optional argument preprocessor.
type AgentTool struct {
	ai.Tool
	Label string

	// Execute runs the tool. signal is the active run's cancellation
	// context. onUpdate, if non-nil, is called with partial results.
	Execute func(toolCallID string, params map[string]any, signal context.Context, onUpdate AgentToolUpdateFunc) (AgentToolResult, error)

	// PrepareArguments transforms the raw arguments before validation and
	// dispatch. Optional.
	PrepareArguments func(args map[string]any) map[string]any
}

// AgentState is the full agent state. Manipulated only from the agent's
// internal goroutine; a Clone snapshot is what callers observe.
type AgentState struct {
	SystemPrompt     string
	Model            ai.Model
	ThinkingLevel    ai.ThinkingLevel
	Tools            []AgentTool
	Messages         []AgentMessage
	IsStreaming      bool
	StreamingMessage AgentMessage
	PendingToolCalls map[string]bool
	ErrorMessage     string
}

// NewAgentState returns a zero state with the pending-tool-calls map
// initialised.
func NewAgentState() *AgentState {
	return &AgentState{
		PendingToolCalls: make(map[string]bool),
	}
}

// Clone returns a shallow-copy snapshot of the state. Slices and the
// pending-calls map are copied so that the snapshot is independent from
// subsequent state mutations.
func (s *AgentState) Clone() *AgentState {
	if s == nil {
		return nil
	}
	clone := &AgentState{
		SystemPrompt:     s.SystemPrompt,
		Model:            s.Model,
		ThinkingLevel:    s.ThinkingLevel,
		IsStreaming:      s.IsStreaming,
		StreamingMessage: s.StreamingMessage,
		ErrorMessage:     s.ErrorMessage,
		PendingToolCalls: make(map[string]bool, len(s.PendingToolCalls)),
	}
	if len(s.Tools) > 0 {
		clone.Tools = make([]AgentTool, len(s.Tools))
		copy(clone.Tools, s.Tools)
	}
	if len(s.Messages) > 0 {
		clone.Messages = make([]AgentMessage, len(s.Messages))
		copy(clone.Messages, s.Messages)
	}
	for k, v := range s.PendingToolCalls {
		clone.PendingToolCalls[k] = v
	}
	return clone
}

// ---- liveness ----

// AgentStatus describes what the agent is doing right now.
type AgentStatus int

const (
	StatusIdle AgentStatus = iota
	StatusThinking
	StatusExecuting
	StatusStalled
)

func (s AgentStatus) String() string {
	switch s {
	case StatusIdle:
		return "idle"
	case StatusThinking:
		return "thinking"
	case StatusExecuting:
		return "executing"
	case StatusStalled:
		return "stalled"
	}
	return "unknown"
}

// LivenessEvent reports the agent's current status and elapsed time in it.
type LivenessEvent struct {
	Status   AgentStatus
	ToolName string
	Elapsed  time.Duration
}

// ---- agent events ----

// AgentEventType is the tag for each AgentEvent.
type AgentEventType string

const (
	AgentEventStart     AgentEventType = "agent_start"
	AgentEventEnd       AgentEventType = "agent_end"
	TurnStart           AgentEventType = "turn_start"
	TurnEnd             AgentEventType = "turn_end"
	MessageStart        AgentEventType = "message_start"
	MessageUpdate       AgentEventType = "message_update"
	MessageEnd          AgentEventType = "message_end"
	ToolExecutionStart  AgentEventType = "tool_execution_start"
	ToolExecutionUpdate AgentEventType = "tool_execution_update"
	ToolExecutionEnd    AgentEventType = "tool_execution_end"
	LivenessUpdate      AgentEventType = "liveness_update"
)

// AgentEvent is a single observation emitted by the agent loop.
type AgentEvent struct {
	Type AgentEventType

	// agent_end
	NewMessages []AgentMessage

	// message_start / message_update / message_end
	Message AgentMessage

	// message_update: the underlying LLM streaming event, if any
	AssistantMessageEvent *ai.AssistantMessageEvent

	// turn_end
	TurnMessage AgentMessage
	ToolResults []ai.ToolResultMessage

	// tool_execution_*
	ToolCallID  string
	ToolName    string
	ToolArgs    map[string]any
	ToolResult  *AgentToolResult
	ToolIsError bool

	// liveness_update
	Liveness *LivenessEvent
}

// ---- hooks ----

// BeforeToolCallContext is passed to a BeforeToolCall hook.
type BeforeToolCallContext struct {
	AssistantMessage *ai.AssistantMessage
	ToolCall         ai.ToolCall
	Args             map[string]any
	Context          *AgentState
}

// BeforeToolCallResult lets a hook block a pending tool call.
type BeforeToolCallResult struct {
	Block  bool
	Reason string
}

// AfterToolCallContext is passed to an AfterToolCall hook after a tool has
// finished (or was blocked).
type AfterToolCallContext struct {
	BeforeToolCallContext
	Result  AgentToolResult
	IsError bool
}

// AfterToolCallResult lets a hook override a tool result. Nil fields leave
// the existing value in place.
type AfterToolCallResult struct {
	Content []ai.Content
	Details any
	IsError *bool
}

// ConvertToLLMFunc lets an application translate custom AgentMessage types
// into LLM messages before a turn.
type ConvertToLLMFunc func(messages []AgentMessage) []ai.Message

// TransformContextFunc is an asynchronous hook applied to the message list
// immediately before ConvertToLLM. It may fetch data, inject memory, or
// otherwise reshape the transcript.
type TransformContextFunc func(ctx context.Context, messages []AgentMessage) ([]AgentMessage, error)

// GetAPIKeyFunc resolves an API key for a provider.
type GetAPIKeyFunc func(provider ai.Provider) string

// BeforeToolCallFunc and AfterToolCallFunc are the tool lifecycle hooks.
type (
	BeforeToolCallFunc func(ctx BeforeToolCallContext) BeforeToolCallResult
	AfterToolCallFunc  func(ctx AfterToolCallContext) AfterToolCallResult
)

// ---- config ----

// ToolExecutionMode selects between parallel and sequential tool dispatch
// inside a single turn.
type ToolExecutionMode string

const (
	ToolExecParallel   ToolExecutionMode = "parallel"
	ToolExecSequential ToolExecutionMode = "sequential"
)

// QueueMode selects how steering and follow-up queues drain.
type QueueMode string

const (
	QueueAll        QueueMode = "all"
	QueueOneAtATime QueueMode = "one-at-a-time"
)

// AgentConfig bundles everything the Agent constructor accepts. All fields
// are optional; callers use struct literals to set the ones they care about.
type AgentConfig struct {
	ConvertToLLM     ConvertToLLMFunc
	TransformContext TransformContextFunc
	GetAPIKey        GetAPIKeyFunc
	BeforeToolCall   BeforeToolCallFunc
	AfterToolCall    AfterToolCallFunc
	ToolExecution    ToolExecutionMode
	SteeringMode     QueueMode
	FollowUpMode     QueueMode
	StreamFn         ai.SimpleStreamFunc
	ThinkingBudgets  *ai.ThinkingBudgets

	// Liveness stall thresholds.
	ThinkingStallTimeout time.Duration
	ToolStallTimeout     time.Duration
}
