package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gmoigneu/gcode/pkg/ai"
	"github.com/gmoigneu/gcode/pkg/ai/providers"
)

// ----- helpers -----

// fauxStream wraps a providers.FauxProvider as an ai.SimpleStreamFunc.
func fauxStream(responses ...providers.FauxResponse) ai.SimpleStreamFunc {
	fp := &providers.FauxProvider{Responses: responses}
	return fp.StreamSimple
}

type collected struct {
	mu     sync.Mutex
	events []AgentEvent
}

func collectEvents(a *Agent) *collected {
	c := &collected{}
	a.Subscribe(func(e AgentEvent, _ context.Context) {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.events = append(c.events, e)
	})
	return c
}

func (c *collected) types() []AgentEventType {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]AgentEventType, len(c.events))
	for i, e := range c.events {
		out[i] = e.Type
	}
	return out
}

func (c *collected) count(t AgentEventType) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, e := range c.events {
		if e.Type == t {
			n++
		}
	}
	return n
}

// ----- single-turn text response -----

func TestLoopSingleTurnTextResponse(t *testing.T) {
	a := New(AgentConfig{
		StreamFn: fauxStream(providers.FauxResponse{Text: "hello"}),
	})
	a.SetModel(ai.Model{ID: "m", Api: "faux"}, ai.ThinkingOff)

	c := collectEvents(a)

	if err := a.Run("hi"); err != nil {
		t.Fatal(err)
	}

	if c.count(AgentEventStart) != 1 || c.count(AgentEventEnd) != 1 {
		t.Errorf("expected one start + one end, got %v", c.types())
	}
	if c.count(TurnEnd) < 1 {
		t.Errorf("no turn_end: %v", c.types())
	}

	state := a.State()
	// initial user + assistant response
	if len(state.Messages) < 2 {
		t.Errorf("messages = %d, want >= 2", len(state.Messages))
	}
}

// ----- tool use turn -----

func TestLoopToolUseTurn(t *testing.T) {
	a := New(AgentConfig{
		StreamFn: fauxStream(
			providers.FauxResponse{
				ToolCalls: []ai.ToolCall{{ID: "c1", Name: "echo", Arguments: map[string]any{"text": "hi"}}},
			},
			providers.FauxResponse{Text: "done"},
		),
	})
	a.SetModel(ai.Model{ID: "m", Api: "faux"}, ai.ThinkingOff)
	a.SetTools([]AgentTool{{
		Tool:  ai.Tool{Name: "echo"},
		Label: "echo",
		Execute: func(id string, params map[string]any, signal context.Context, up AgentToolUpdateFunc) (AgentToolResult, error) {
			return AgentToolResult{Content: []ai.Content{&ai.TextContent{Text: fmt.Sprintf("echoed: %v", params["text"])}}}, nil
		},
	}})

	c := collectEvents(a)

	if err := a.Run("please echo"); err != nil {
		t.Fatal(err)
	}

	if c.count(ToolExecutionStart) != 1 || c.count(ToolExecutionEnd) != 1 {
		t.Errorf("tool execution events missing: %v", c.types())
	}

	// Verify the assistant + tool result are both in the transcript.
	state := a.State()
	var hasAssistant, hasToolResult bool
	for _, m := range state.Messages {
		switch m.(type) {
		case *ai.AssistantMessage:
			hasAssistant = true
		case *ai.ToolResultMessage:
			hasToolResult = true
		}
	}
	if !hasAssistant || !hasToolResult {
		t.Errorf("transcript missing pieces: %+v", state.Messages)
	}
}

// ----- tool unknown -----

func TestLoopUnknownToolGetsErrorResult(t *testing.T) {
	a := New(AgentConfig{
		StreamFn: fauxStream(
			providers.FauxResponse{
				ToolCalls: []ai.ToolCall{{ID: "c1", Name: "does_not_exist"}},
			},
			providers.FauxResponse{Text: "ok"},
		),
	})
	a.SetModel(ai.Model{ID: "m", Api: "faux"}, ai.ThinkingOff)

	if err := a.Run("go"); err != nil {
		t.Fatal(err)
	}
	state := a.State()
	var tr *ai.ToolResultMessage
	for _, m := range state.Messages {
		if t, ok := m.(*ai.ToolResultMessage); ok {
			tr = t
			break
		}
	}
	if tr == nil || !tr.IsError {
		t.Fatalf("expected error tool result, got %+v", tr)
	}
	if len(tr.Content) == 0 {
		t.Errorf("error result empty")
	}
}

// ----- BeforeToolCall block -----

func TestLoopBeforeToolCallBlock(t *testing.T) {
	a := New(AgentConfig{
		StreamFn: fauxStream(
			providers.FauxResponse{
				ToolCalls: []ai.ToolCall{{ID: "c1", Name: "sensitive"}},
			},
			providers.FauxResponse{Text: "ok"},
		),
		BeforeToolCall: func(ctx BeforeToolCallContext) BeforeToolCallResult {
			return BeforeToolCallResult{Block: true, Reason: "not allowed"}
		},
	})
	a.SetModel(ai.Model{ID: "m", Api: "faux"}, ai.ThinkingOff)
	a.SetTools([]AgentTool{{
		Tool: ai.Tool{Name: "sensitive"},
		Execute: func(id string, params map[string]any, signal context.Context, up AgentToolUpdateFunc) (AgentToolResult, error) {
			t.Fatal("Execute should not be called for a blocked tool")
			return AgentToolResult{}, nil
		},
	}})

	if err := a.Run("try"); err != nil {
		t.Fatal(err)
	}

	state := a.State()
	var tr *ai.ToolResultMessage
	for _, m := range state.Messages {
		if t, ok := m.(*ai.ToolResultMessage); ok {
			tr = t
		}
	}
	if tr == nil || !tr.IsError {
		t.Fatalf("expected blocked error result, got %+v", tr)
	}
	if tc := tr.Content[0].(*ai.TextContent); tc.Text != "not allowed" {
		t.Errorf("reason = %q", tc.Text)
	}
}

// ----- parallel execution preserves order -----

func TestLoopParallelToolExecutionPreservesOrder(t *testing.T) {
	a := New(AgentConfig{
		StreamFn: fauxStream(
			providers.FauxResponse{
				ToolCalls: []ai.ToolCall{
					{ID: "c1", Name: "slow"},
					{ID: "c2", Name: "fast"},
				},
			},
			providers.FauxResponse{Text: "done"},
		),
		ToolExecution: ToolExecParallel,
	})
	a.SetModel(ai.Model{ID: "m", Api: "faux"}, ai.ThinkingOff)

	slowCalls := make(chan string, 4)
	a.SetTools([]AgentTool{
		{
			Tool: ai.Tool{Name: "slow"},
			Execute: func(id string, p map[string]any, sig context.Context, up AgentToolUpdateFunc) (AgentToolResult, error) {
				time.Sleep(30 * time.Millisecond)
				slowCalls <- "slow-done"
				return AgentToolResult{Content: []ai.Content{&ai.TextContent{Text: "slow"}}}, nil
			},
		},
		{
			Tool: ai.Tool{Name: "fast"},
			Execute: func(id string, p map[string]any, sig context.Context, up AgentToolUpdateFunc) (AgentToolResult, error) {
				slowCalls <- "fast-done"
				return AgentToolResult{Content: []ai.Content{&ai.TextContent{Text: "fast"}}}, nil
			},
		},
	})

	if err := a.Run("go"); err != nil {
		t.Fatal(err)
	}

	// Transcript: the tool results for c1 and c2 should appear in source order.
	state := a.State()
	var seen []string
	for _, m := range state.Messages {
		if tr, ok := m.(*ai.ToolResultMessage); ok {
			seen = append(seen, tr.ToolCallID)
		}
	}
	if len(seen) < 2 || seen[0] != "c1" || seen[1] != "c2" {
		t.Errorf("expected c1 before c2, got %v", seen)
	}

	// Fast finished before slow based on the channel order.
	if len(slowCalls) < 2 {
		t.Errorf("expected two tool invocations, got %d", len(slowCalls))
	}
	first := <-slowCalls
	if first != "fast-done" {
		t.Errorf("fast should finish first in real time, got %q", first)
	}
}

// ----- sequential execution -----

func TestLoopSequentialToolExecution(t *testing.T) {
	a := New(AgentConfig{
		StreamFn: fauxStream(
			providers.FauxResponse{
				ToolCalls: []ai.ToolCall{
					{ID: "c1", Name: "tool"},
					{ID: "c2", Name: "tool"},
				},
			},
			providers.FauxResponse{Text: "ok"},
		),
		ToolExecution: ToolExecSequential,
	})
	a.SetModel(ai.Model{ID: "m", Api: "faux"}, ai.ThinkingOff)

	var order []string
	var mu sync.Mutex
	a.SetTools([]AgentTool{{
		Tool: ai.Tool{Name: "tool"},
		Execute: func(id string, p map[string]any, sig context.Context, up AgentToolUpdateFunc) (AgentToolResult, error) {
			mu.Lock()
			order = append(order, id)
			mu.Unlock()
			return AgentToolResult{Content: []ai.Content{&ai.TextContent{Text: id}}}, nil
		},
	}})

	if err := a.Run("go"); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != "c1" || order[1] != "c2" {
		t.Errorf("sequential order = %v", order)
	}
}

// ----- steering injection -----

func TestLoopSteeringProcessedAfterTurn(t *testing.T) {
	var turnCount int
	streamFn := func(model ai.Model, ctx ai.Context, opts *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		turnCount++
		fp := &providers.FauxProvider{Responses: []providers.FauxResponse{
			{Text: fmt.Sprintf("turn-%d", turnCount)},
		}}
		return fp.StreamSimple(model, ctx, opts)
	}

	a := New(AgentConfig{StreamFn: streamFn})
	a.SetModel(ai.Model{ID: "m", Api: "faux"}, ai.ThinkingOff)

	// Subscribe to inject a steering message right after the first message ends.
	var injected bool
	a.Subscribe(func(e AgentEvent, _ context.Context) {
		if e.Type == MessageEnd && !injected {
			if asst, ok := e.Message.(*ai.AssistantMessage); ok && asst.Model != "" {
				injected = true
				a.Steer(&ai.UserMessage{
					Content:   []ai.Content{&ai.TextContent{Text: "steered"}},
					Timestamp: time.Now().UnixMilli(),
				})
			}
		}
	})

	if err := a.Run("start"); err != nil {
		t.Fatal(err)
	}

	if turnCount < 2 {
		t.Errorf("steering should trigger a second turn, got turns=%d", turnCount)
	}
}

// ----- follow-up queue -----

func TestLoopFollowUpQueue(t *testing.T) {
	a := New(AgentConfig{
		StreamFn: fauxStream(
			providers.FauxResponse{Text: "first"},
			providers.FauxResponse{Text: "second"},
		),
	})
	a.SetModel(ai.Model{ID: "m", Api: "faux"}, ai.ThinkingOff)

	a.FollowUp(&ai.UserMessage{
		Content:   []ai.Content{&ai.TextContent{Text: "more"}},
		Timestamp: time.Now().UnixMilli(),
	})

	if err := a.Run("hi"); err != nil {
		t.Fatal(err)
	}

	// After first turn completes (no tool calls), the follow-up should kick
	// off a second turn.
	state := a.State()
	assistantCount := 0
	for _, m := range state.Messages {
		if _, ok := m.(*ai.AssistantMessage); ok {
			assistantCount++
		}
	}
	if assistantCount < 2 {
		t.Errorf("follow-up should have caused a second assistant turn; got %d", assistantCount)
	}
}

// ----- LLM error terminates loop -----

func TestLoopErrorStopReasonTerminatesLoop(t *testing.T) {
	a := New(AgentConfig{
		StreamFn: func(model ai.Model, ctx ai.Context, opts *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			s := ai.NewAssistantMessageEventStream()
			go func() {
				s.Push(ai.AssistantMessageEvent{
					Type: ai.EventError,
					Error: &ai.AssistantMessage{
						Model:        "m",
						StopReason:   ai.StopReasonError,
						ErrorMessage: "boom",
						Timestamp:    time.Now().UnixMilli(),
					},
				})
			}()
			return s
		},
	})
	a.SetModel(ai.Model{ID: "m", Api: "faux"}, ai.ThinkingOff)

	err := a.Run("hi")
	if err == nil || err.Error() != "agent: boom" {
		t.Errorf("err = %v", err)
	}
	if a.State().ErrorMessage != "boom" {
		t.Errorf("state error = %q", a.State().ErrorMessage)
	}
}

// ----- cancellation during loop -----

func TestLoopAbortCancelsStream(t *testing.T) {
	a := New(AgentConfig{
		StreamFn: func(model ai.Model, ctx ai.Context, opts *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			s := ai.NewAssistantMessageEventStream()
			go func() {
				<-opts.Signal.Done()
				s.Push(ai.AssistantMessageEvent{
					Type: ai.EventError,
					Error: &ai.AssistantMessage{
						Model:        "m",
						StopReason:   ai.StopReasonAborted,
						ErrorMessage: "cancelled",
						Timestamp:    time.Now().UnixMilli(),
					},
				})
			}()
			return s
		},
	})
	a.SetModel(ai.Model{ID: "m", Api: "faux"}, ai.ThinkingOff)

	done := make(chan error, 1)
	go func() { done <- a.Run("hang") }()
	time.Sleep(20 * time.Millisecond)
	a.Abort()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("abort did not terminate loop")
	}
}

// ----- default ConvertToLLM filters errors -----

func TestDefaultConvertToLLMFiltersErrors(t *testing.T) {
	msgs := []AgentMessage{
		&ai.UserMessage{Content: []ai.Content{&ai.TextContent{Text: "hi"}}},
		&ai.AssistantMessage{StopReason: ai.StopReasonError},
		&ai.AssistantMessage{StopReason: ai.StopReasonAborted},
		&ai.AssistantMessage{StopReason: ai.StopReasonStop, Content: []ai.Content{&ai.TextContent{Text: "ok"}}},
		&ai.ToolResultMessage{ToolCallID: "c1"},
	}
	out := defaultConvertToLLM(msgs)
	if len(out) != 3 {
		t.Errorf("len = %d", len(out))
	}
}

// ----- extractToolCalls -----

func TestExtractToolCalls(t *testing.T) {
	m := &ai.AssistantMessage{
		Content: []ai.Content{
			&ai.TextContent{Text: "hi"},
			&ai.ToolCall{ID: "c1", Name: "read"},
			&ai.TextContent{Text: "ok"},
			&ai.ToolCall{ID: "c2", Name: "write"},
		},
	}
	out := extractToolCalls(m)
	if len(out) != 2 || out[0].ID != "c1" || out[1].ID != "c2" {
		t.Errorf("got %+v", out)
	}
}
