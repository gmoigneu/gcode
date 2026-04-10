package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gmoigneu/gcode/pkg/ai"
)

func TestNewAgentDefaults(t *testing.T) {
	a := New(AgentConfig{})
	cfg := a.Config()
	if cfg.SteeringMode != QueueOneAtATime {
		t.Errorf("steering mode = %q", cfg.SteeringMode)
	}
	if cfg.FollowUpMode != QueueOneAtATime {
		t.Errorf("followup mode = %q", cfg.FollowUpMode)
	}
	if cfg.ToolExecution != ToolExecParallel {
		t.Errorf("tool exec = %q", cfg.ToolExecution)
	}
	state := a.State()
	if state == nil || len(state.Messages) != 0 {
		t.Errorf("state = %+v", state)
	}
}

func TestAgentStateSnapshot(t *testing.T) {
	a := New(AgentConfig{})
	a.SetSystemPrompt("you are helpful")
	a.SetTools([]AgentTool{{Label: "read"}})

	snap := a.State()
	if snap.SystemPrompt != "you are helpful" {
		t.Errorf("snap.SystemPrompt = %q", snap.SystemPrompt)
	}
	if len(snap.Tools) != 1 {
		t.Errorf("snap.Tools = %v", snap.Tools)
	}
	// Mutating the snapshot should not affect the agent.
	snap.SystemPrompt = "hacked"
	snap.Tools = append(snap.Tools, AgentTool{Label: "x"})
	if a.State().SystemPrompt != "you are helpful" {
		t.Error("agent state mutated via snapshot")
	}
	if len(a.State().Tools) != 1 {
		t.Error("agent tools mutated via snapshot")
	}
}

func TestAgentSubscribeAndUnsubscribe(t *testing.T) {
	// Stub the loop so tests don't depend on ai.StreamSimple.
	prev := runLoopFn
	t.Cleanup(func() { runLoopFn = prev })
	runLoopFn = func(ctx context.Context, agent *Agent, initial []AgentMessage) error {
		agent.emit(AgentEvent{Type: AgentEventStart}, ctx)
		for _, m := range initial {
			agent.emit(AgentEvent{Type: MessageStart, Message: m}, ctx)
			agent.emit(AgentEvent{Type: MessageEnd, Message: m}, ctx)
		}
		agent.emit(AgentEvent{Type: AgentEventEnd}, ctx)
		return nil
	}

	a := New(AgentConfig{})

	var mu sync.Mutex
	var received []AgentEventType
	unsub := a.Subscribe(func(e AgentEvent, _ context.Context) {
		mu.Lock()
		defer mu.Unlock()
		received = append(received, e.Type)
	})

	if err := a.Run("hello"); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	before := append([]AgentEventType(nil), received...)
	mu.Unlock()

	if len(before) == 0 {
		t.Fatal("no events delivered")
	}
	// Default stub emits at least agent_start, message_start, message_end, agent_end.
	if before[0] != AgentEventStart || before[len(before)-1] != AgentEventEnd {
		t.Errorf("sequence = %v", before)
	}

	unsub()
	if err := a.Run("second"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	after := append([]AgentEventType(nil), received...)
	mu.Unlock()
	if len(after) != len(before) {
		t.Errorf("listener received events after unsubscribe: before=%d after=%d", len(before), len(after))
	}
}

func TestAgentRunRejectsConcurrent(t *testing.T) {
	// Install a slow stub loop so the first run is definitely still active.
	prev := runLoopFn
	t.Cleanup(func() { runLoopFn = prev })

	started := make(chan struct{})
	release := make(chan struct{})
	runLoopFn = func(ctx context.Context, agent *Agent, initial []AgentMessage) error {
		close(started)
		<-release
		return nil
	}

	a := New(AgentConfig{})
	errCh := make(chan error, 1)
	go func() { errCh <- a.Run("first") }()

	<-started
	if err := a.Run("second"); err == nil {
		t.Error("expected error when running concurrently")
	}

	close(release)
	if err := <-errCh; err != nil {
		t.Errorf("first run returned error: %v", err)
	}
}

func TestAgentAbortCancelsRun(t *testing.T) {
	prev := runLoopFn
	t.Cleanup(func() { runLoopFn = prev })

	runLoopFn = func(ctx context.Context, agent *Agent, initial []AgentMessage) error {
		<-ctx.Done()
		return ctx.Err()
	}

	a := New(AgentConfig{})
	done := make(chan error, 1)
	go func() { done <- a.Run("hang") }()

	// Give Run a moment to begin.
	time.Sleep(10 * time.Millisecond)
	a.Abort()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("run error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Abort did not cancel run")
	}
}

func TestAgentWaitForIdleNoRun(t *testing.T) {
	a := New(AgentConfig{})
	done := make(chan struct{})
	go func() {
		a.WaitForIdle()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("WaitForIdle blocked when no run is active")
	}
}

func TestAgentWaitForIdleCompletesAfterRun(t *testing.T) {
	prev := runLoopFn
	t.Cleanup(func() { runLoopFn = prev })

	release := make(chan struct{})
	runLoopFn = func(ctx context.Context, agent *Agent, initial []AgentMessage) error {
		<-release
		return nil
	}

	a := New(AgentConfig{})
	go a.Run("x")
	time.Sleep(5 * time.Millisecond)

	waited := make(chan struct{})
	go func() {
		a.WaitForIdle()
		close(waited)
	}()

	select {
	case <-waited:
		t.Fatal("WaitForIdle returned before run completed")
	case <-time.After(20 * time.Millisecond):
	}

	close(release)
	select {
	case <-waited:
	case <-time.After(time.Second):
		t.Fatal("WaitForIdle did not return after run completed")
	}
}

func TestAgentReset(t *testing.T) {
	a := New(AgentConfig{})
	a.SetSystemPrompt("prompt")
	a.Steer(msg("a", 1))
	a.FollowUp(msg("b", 2))

	a.Reset()

	state := a.State()
	if state.SystemPrompt != "" {
		t.Errorf("systemPrompt after reset = %q", state.SystemPrompt)
	}
	if a.SteeringQueue().HasItems() {
		t.Error("steering queue not cleared")
	}
	if a.FollowUpQueue().HasItems() {
		t.Error("follow-up queue not cleared")
	}
}

func TestAgentProcessEventStateReducer(t *testing.T) {
	a := New(AgentConfig{})

	a.processEvent(AgentEvent{Type: MessageStart, Message: &ai.UserMessage{}})
	if a.state.StreamingMessage == nil {
		t.Error("StreamingMessage should be set on MessageStart")
	}
	a.processEvent(AgentEvent{Type: MessageEnd, Message: &ai.UserMessage{}})
	if a.state.StreamingMessage != nil {
		t.Error("StreamingMessage should be cleared on MessageEnd")
	}
	a.processEvent(AgentEvent{Type: ToolExecutionStart, ToolCallID: "c1"})
	if !a.state.PendingToolCalls["c1"] {
		t.Error("pending calls should track tool call")
	}
	a.processEvent(AgentEvent{Type: ToolExecutionEnd, ToolCallID: "c1"})
	if a.state.PendingToolCalls["c1"] {
		t.Error("pending calls should clear on tool end")
	}

	errMsg := &ai.AssistantMessage{StopReason: ai.StopReasonError, ErrorMessage: "boom"}
	a.processEvent(AgentEvent{Type: TurnEnd, TurnMessage: errMsg})
	if a.state.ErrorMessage != "boom" {
		t.Errorf("errorMessage = %q", a.state.ErrorMessage)
	}
}

func TestAgentRunBuildsUserMessage(t *testing.T) {
	prev := runLoopFn
	t.Cleanup(func() { runLoopFn = prev })

	var got []AgentMessage
	runLoopFn = func(ctx context.Context, agent *Agent, initial []AgentMessage) error {
		got = initial
		return nil
	}

	a := New(AgentConfig{})
	img := ai.ImageContent{Data: "AAAA", MimeType: "image/png"}
	if err := a.Run("look", img); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("initial messages = %d", len(got))
	}
	um, ok := got[0].(*ai.UserMessage)
	if !ok {
		t.Fatalf("got %T", got[0])
	}
	if len(um.Content) != 2 {
		t.Errorf("content len = %d", len(um.Content))
	}
	if tc, ok := um.Content[0].(*ai.TextContent); !ok || tc.Text != "look" {
		t.Errorf("content[0] = %+v", um.Content[0])
	}
	if _, ok := um.Content[1].(*ai.ImageContent); !ok {
		t.Errorf("content[1] = %T", um.Content[1])
	}
}

func TestAgentRunEmptyInput(t *testing.T) {
	prev := runLoopFn
	t.Cleanup(func() { runLoopFn = prev })

	var got []AgentMessage
	runLoopFn = func(ctx context.Context, agent *Agent, initial []AgentMessage) error {
		got = initial
		return nil
	}

	a := New(AgentConfig{})
	if err := a.Run(""); err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("empty input should yield nil initial messages, got %v", got)
	}
}
