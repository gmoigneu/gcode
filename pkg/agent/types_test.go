package agent

import (
	"testing"
	"time"

	"github.com/gmoigneu/gcode/pkg/ai"
)

func TestAIMessagesSatisfyAgentMessage(t *testing.T) {
	var _ AgentMessage = (*ai.UserMessage)(nil)
	var _ AgentMessage = (*ai.AssistantMessage)(nil)
	var _ AgentMessage = (*ai.ToolResultMessage)(nil)
}

func TestAgentStateClonesSlices(t *testing.T) {
	s := NewAgentState()
	s.SystemPrompt = "hello"
	s.Messages = []AgentMessage{
		&ai.UserMessage{Timestamp: 1, Content: []ai.Content{&ai.TextContent{Text: "hi"}}},
	}
	s.Tools = []AgentTool{{Label: "read"}}
	s.PendingToolCalls["call_1"] = true
	s.StreamingMessage = s.Messages[0]

	clone := s.Clone()
	if clone == s {
		t.Error("Clone returned the same pointer")
	}
	if clone.SystemPrompt != "hello" {
		t.Errorf("system prompt = %q", clone.SystemPrompt)
	}
	if len(clone.Messages) != 1 || len(clone.Tools) != 1 {
		t.Errorf("slices not copied: %+v", clone)
	}
	if !clone.PendingToolCalls["call_1"] {
		t.Errorf("pending calls map not copied")
	}

	// Mutating the original must not affect the clone.
	s.Messages = append(s.Messages, &ai.UserMessage{Timestamp: 2})
	s.Tools = append(s.Tools, AgentTool{Label: "bash"})
	s.PendingToolCalls["call_2"] = true
	if len(clone.Messages) != 1 {
		t.Errorf("clone messages mutated")
	}
	if len(clone.Tools) != 1 {
		t.Errorf("clone tools mutated")
	}
	if clone.PendingToolCalls["call_2"] {
		t.Errorf("clone pending calls mutated")
	}
}

func TestAgentStateCloneNil(t *testing.T) {
	var s *AgentState
	if got := s.Clone(); got != nil {
		t.Errorf("Clone(nil) = %v", got)
	}
}

func TestAgentStatusString(t *testing.T) {
	cases := map[AgentStatus]string{
		StatusIdle:      "idle",
		StatusThinking:  "thinking",
		StatusExecuting: "executing",
		StatusStalled:   "stalled",
	}
	for s, want := range cases {
		if s.String() != want {
			t.Errorf("%d.String() = %q, want %q", s, s.String(), want)
		}
	}
}

func TestLivenessEventShape(t *testing.T) {
	e := &LivenessEvent{Status: StatusExecuting, ToolName: "read", Elapsed: 2 * time.Second}
	if e.Status != StatusExecuting || e.ToolName != "read" || e.Elapsed != 2*time.Second {
		t.Errorf("got %+v", e)
	}
}

func TestAgentEventConstants(t *testing.T) {
	// Spot-check a few constants to catch typos.
	if AgentEventStart != "agent_start" {
		t.Errorf("start = %q", AgentEventStart)
	}
	if LivenessUpdate != "liveness_update" {
		t.Errorf("liveness = %q", LivenessUpdate)
	}
	if ToolExecutionStart != "tool_execution_start" {
		t.Errorf("exec start = %q", ToolExecutionStart)
	}
}

func TestAgentConfigZeroValue(t *testing.T) {
	var c AgentConfig
	// Zero-value config must be usable (defaults applied elsewhere).
	if c.ToolExecution != "" || c.SteeringMode != "" {
		t.Errorf("unexpected non-zero default: %+v", c)
	}
}
