package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gmoigneu/gcode/pkg/ai"
	"github.com/gmoigneu/gcode/pkg/ai/providers"
)

// ----- fast tick interval for tests -----

func withFastLivenessTicks(t *testing.T) {
	t.Helper()
	prev := livenessTickInterval
	livenessTickInterval = 5 * time.Millisecond
	t.Cleanup(func() { livenessTickInterval = prev })
}

// ----- helpers -----

type livenessCollector struct {
	mu     sync.Mutex
	events []LivenessEvent
}

func (c *livenessCollector) observe(e AgentEvent, _ context.Context) {
	if e.Type != LivenessUpdate || e.Liveness == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, *e.Liveness)
}

func (c *livenessCollector) snapshot() []LivenessEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]LivenessEvent(nil), c.events...)
}

func (c *livenessCollector) statuses() []AgentStatus {
	evs := c.snapshot()
	out := make([]AgentStatus, len(evs))
	for i, e := range evs {
		out[i] = e.Status
	}
	return out
}

// ----- tests -----

func TestLivenessSingleTurnThinkingToIdle(t *testing.T) {
	withFastLivenessTicks(t)

	a := New(AgentConfig{
		StreamFn: (&providers.FauxProvider{Responses: []providers.FauxResponse{{Text: "ok"}}}).StreamSimple,
	})
	a.SetModel(ai.Model{ID: "m", Api: "faux"}, ai.ThinkingOff)

	c := &livenessCollector{}
	a.Subscribe(c.observe)

	if err := a.Run("hi"); err != nil {
		t.Fatal(err)
	}

	statuses := c.statuses()
	if len(statuses) < 2 {
		t.Fatalf("too few liveness events: %v", statuses)
	}
	if statuses[0] != StatusThinking {
		t.Errorf("first = %v, want thinking", statuses[0])
	}
	if statuses[len(statuses)-1] != StatusIdle {
		t.Errorf("last = %v, want idle", statuses[len(statuses)-1])
	}
}

func TestLivenessThinkingThenExecutingThenIdle(t *testing.T) {
	withFastLivenessTicks(t)

	a := New(AgentConfig{
		StreamFn: (&providers.FauxProvider{Responses: []providers.FauxResponse{
			{ToolCalls: []ai.ToolCall{{ID: "c1", Name: "read"}}},
			{Text: "done"},
		}}).StreamSimple,
	})
	a.SetModel(ai.Model{ID: "m", Api: "faux"}, ai.ThinkingOff)
	a.SetTools([]AgentTool{{
		Tool: ai.Tool{Name: "read"},
		Execute: func(id string, p map[string]any, sig context.Context, up AgentToolUpdateFunc) (AgentToolResult, error) {
			return AgentToolResult{Content: []ai.Content{&ai.TextContent{Text: "ok"}}}, nil
		},
	}})

	c := &livenessCollector{}
	a.Subscribe(c.observe)

	if err := a.Run("hi"); err != nil {
		t.Fatal(err)
	}

	// Expected subsequence somewhere in the collected events:
	// thinking -> executing (tool name "read") -> thinking -> idle
	events := c.snapshot()
	if len(events) == 0 {
		t.Fatal("no events")
	}
	var sawThinking, sawExecuting, sawIdle bool
	var executingToolName string
	for _, e := range events {
		switch e.Status {
		case StatusThinking:
			sawThinking = true
		case StatusExecuting:
			sawExecuting = true
			if e.ToolName != "" {
				executingToolName = e.ToolName
			}
		case StatusIdle:
			sawIdle = true
		}
	}
	if !sawThinking || !sawExecuting || !sawIdle {
		t.Errorf("missing statuses: thinking=%v executing=%v idle=%v", sawThinking, sawExecuting, sawIdle)
	}
	if executingToolName != "read" {
		t.Errorf("executing tool name = %q", executingToolName)
	}
	if events[len(events)-1].Status != StatusIdle {
		t.Errorf("final = %v", events[len(events)-1].Status)
	}
}

func TestLivenessElapsedMonotonic(t *testing.T) {
	withFastLivenessTicks(t)

	// Stream that blocks long enough to see multiple ticks.
	blockCh := make(chan struct{})
	streamFn := func(model ai.Model, ctx ai.Context, opts *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		s := ai.NewAssistantMessageEventStream()
		go func() {
			<-blockCh
			s.Push(ai.AssistantMessageEvent{
				Type:    ai.EventDone,
				Message: &ai.AssistantMessage{Model: "m", StopReason: ai.StopReasonStop},
			})
		}()
		return s
	}

	a := New(AgentConfig{StreamFn: streamFn})
	a.SetModel(ai.Model{ID: "m", Api: "faux"}, ai.ThinkingOff)

	c := &livenessCollector{}
	a.Subscribe(c.observe)

	done := make(chan error, 1)
	go func() { done <- a.Run("hi") }()

	// Let the ticker emit a few events.
	time.Sleep(40 * time.Millisecond)
	close(blockCh)

	if err := <-done; err != nil {
		t.Fatal(err)
	}

	events := c.snapshot()
	// Find consecutive thinking events and check elapsed is non-decreasing.
	var prev time.Duration = -1
	for _, e := range events {
		if e.Status != StatusThinking {
			continue
		}
		if e.Elapsed < prev {
			t.Errorf("elapsed regressed: %v -> %v", prev, e.Elapsed)
		}
		prev = e.Elapsed
	}
}

func TestLivenessStallEscalationThinking(t *testing.T) {
	withFastLivenessTicks(t)

	blockCh := make(chan struct{})
	streamFn := func(model ai.Model, ctx ai.Context, opts *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		s := ai.NewAssistantMessageEventStream()
		go func() {
			<-blockCh
			s.Push(ai.AssistantMessageEvent{
				Type:    ai.EventDone,
				Message: &ai.AssistantMessage{Model: "m", StopReason: ai.StopReasonStop},
			})
		}()
		return s
	}

	a := New(AgentConfig{
		StreamFn:             streamFn,
		ThinkingStallTimeout: 15 * time.Millisecond,
	})
	a.SetModel(ai.Model{ID: "m", Api: "faux"}, ai.ThinkingOff)

	c := &livenessCollector{}
	a.Subscribe(c.observe)

	done := make(chan error, 1)
	go func() { done <- a.Run("hi") }()
	time.Sleep(60 * time.Millisecond) // well past threshold
	close(blockCh)

	if err := <-done; err != nil {
		t.Fatal(err)
	}

	sawStall := false
	for _, e := range c.snapshot() {
		if e.Status == StatusStalled {
			sawStall = true
			break
		}
	}
	if !sawStall {
		t.Error("stall escalation not observed")
	}
	// Stall must not abort — the run should still complete cleanly.
	if a.State().ErrorMessage != "" {
		t.Errorf("stall should not set error: %q", a.State().ErrorMessage)
	}
}

func TestLivenessStallDoesNotAbortTool(t *testing.T) {
	withFastLivenessTicks(t)

	a := New(AgentConfig{
		StreamFn: (&providers.FauxProvider{Responses: []providers.FauxResponse{
			{ToolCalls: []ai.ToolCall{{ID: "c1", Name: "slow"}}},
			{Text: "done"},
		}}).StreamSimple,
		ToolStallTimeout: 10 * time.Millisecond,
	})
	a.SetModel(ai.Model{ID: "m", Api: "faux"}, ai.ThinkingOff)
	a.SetTools([]AgentTool{{
		Tool: ai.Tool{Name: "slow"},
		Execute: func(id string, p map[string]any, sig context.Context, up AgentToolUpdateFunc) (AgentToolResult, error) {
			time.Sleep(40 * time.Millisecond)
			return AgentToolResult{Content: []ai.Content{&ai.TextContent{Text: "ok"}}}, nil
		},
	}})

	c := &livenessCollector{}
	a.Subscribe(c.observe)

	if err := a.Run("hi"); err != nil {
		t.Fatal(err)
	}

	var sawStallExecuting bool
	for _, e := range c.snapshot() {
		if e.Status == StatusStalled {
			sawStallExecuting = true
		}
	}
	if !sawStallExecuting {
		t.Error("tool stall escalation not observed")
	}

	// Tool must still have completed successfully.
	state := a.State()
	var trSeen bool
	for _, m := range state.Messages {
		if tr, ok := m.(*ai.ToolResultMessage); ok && !tr.IsError {
			trSeen = true
		}
	}
	if !trSeen {
		t.Error("expected a successful tool result despite stall")
	}
}

func TestLivenessTickerStopsOnStateChange(t *testing.T) {
	withFastLivenessTicks(t)

	a := New(AgentConfig{
		StreamFn: (&providers.FauxProvider{Responses: []providers.FauxResponse{
			{ToolCalls: []ai.ToolCall{{ID: "c1", Name: "read"}}},
			{Text: "done"},
		}}).StreamSimple,
	})
	a.SetModel(ai.Model{ID: "m", Api: "faux"}, ai.ThinkingOff)
	a.SetTools([]AgentTool{{
		Tool: ai.Tool{Name: "read"},
		Execute: func(id string, p map[string]any, sig context.Context, up AgentToolUpdateFunc) (AgentToolResult, error) {
			time.Sleep(30 * time.Millisecond)
			return AgentToolResult{Content: []ai.Content{&ai.TextContent{Text: "ok"}}}, nil
		},
	}})

	c := &livenessCollector{}
	a.Subscribe(c.observe)

	if err := a.Run("hi"); err != nil {
		t.Fatal(err)
	}

	// After the final idle event, there should be no more events from any
	// earlier ticker goroutine.
	events := c.snapshot()
	if len(events) == 0 {
		t.Fatal("no events")
	}
	if events[len(events)-1].Status != StatusIdle {
		t.Errorf("last event = %v", events[len(events)-1].Status)
	}
	// Wait a bit for any stray goroutine output (there should be none
	// because Stop waits on wg).
	time.Sleep(20 * time.Millisecond)
	afterStop := c.snapshot()
	if len(afterStop) != len(events) {
		t.Errorf("new liveness events after stop: before=%d after=%d", len(events), len(afterStop))
	}
}
