package providers

import (
	"context"
	"testing"
	"time"

	"github.com/gmoigneu/gcode/pkg/ai"
)

func TestFauxEmitsText(t *testing.T) {
	f := &FauxProvider{Responses: []FauxResponse{{Text: "hello"}}}
	stream := f.Stream(ai.Model{ID: "faux-1"}, ai.Context{}, &ai.StreamOptions{})

	var deltas string
	var sawStart, sawEnd, sawDone bool
	for e := range stream.C {
		switch e.Type {
		case ai.EventTextStart:
			sawStart = true
		case ai.EventTextDelta:
			deltas += e.Delta
		case ai.EventTextEnd:
			sawEnd = true
		case ai.EventDone:
			sawDone = true
		}
	}
	if !sawStart || !sawEnd || !sawDone {
		t.Errorf("missing events: start=%v end=%v done=%v", sawStart, sawEnd, sawDone)
	}
	if deltas != "hello" {
		t.Errorf("deltas = %q, want hello", deltas)
	}
	res := stream.Result()
	if res.StopReason != ai.StopReasonStop {
		t.Errorf("stopReason = %q", res.StopReason)
	}
	if res.Model != "faux-1" {
		t.Errorf("model = %q", res.Model)
	}
}

func TestFauxEmitsThinking(t *testing.T) {
	f := &FauxProvider{Responses: []FauxResponse{{Thinking: "plan", Text: "done"}}}
	stream := f.Stream(ai.Model{ID: "m"}, ai.Context{}, nil)

	var thinkDeltas, textDeltas string
	for e := range stream.C {
		switch e.Type {
		case ai.EventThinkingDelta:
			thinkDeltas += e.Delta
		case ai.EventTextDelta:
			textDeltas += e.Delta
		}
	}
	if thinkDeltas != "plan" {
		t.Errorf("thinking deltas = %q", thinkDeltas)
	}
	if textDeltas != "done" {
		t.Errorf("text deltas = %q", textDeltas)
	}
}

func TestFauxEmitsToolCalls(t *testing.T) {
	f := &FauxProvider{Responses: []FauxResponse{{
		ToolCalls: []ai.ToolCall{{
			ID:        "c1",
			Name:      "read",
			Arguments: map[string]any{"path": "/tmp/x"},
		}},
	}}}
	stream := f.Stream(ai.Model{ID: "m"}, ai.Context{}, nil)

	var startCount, endCount int
	var lastCall *ai.ToolCall
	for e := range stream.C {
		switch e.Type {
		case ai.EventToolCallStart:
			startCount++
		case ai.EventToolCallEnd:
			endCount++
			lastCall = e.ToolCall
		}
	}
	if startCount != 1 || endCount != 1 {
		t.Errorf("start/end counts = %d/%d", startCount, endCount)
	}
	if lastCall == nil || lastCall.Name != "read" || lastCall.Arguments["path"] != "/tmp/x" {
		t.Errorf("tool call = %+v", lastCall)
	}

	if stream.Result().StopReason != ai.StopReasonToolUse {
		t.Errorf("expected StopReasonToolUse when tool calls present")
	}
}

func TestFauxCyclesThroughResponses(t *testing.T) {
	f := &FauxProvider{Responses: []FauxResponse{
		{Text: "first"},
		{Text: "second"},
	}}

	collect := func(stream *ai.AssistantMessageEventStream) string {
		var s string
		for e := range stream.C {
			if e.Type == ai.EventTextDelta {
				s += e.Delta
			}
		}
		return s
	}

	if got := collect(f.Stream(ai.Model{ID: "m"}, ai.Context{}, nil)); got != "first" {
		t.Errorf("call 1 = %q", got)
	}
	if got := collect(f.Stream(ai.Model{ID: "m"}, ai.Context{}, nil)); got != "second" {
		t.Errorf("call 2 = %q", got)
	}
	if got := collect(f.Stream(ai.Model{ID: "m"}, ai.Context{}, nil)); got != "first" {
		t.Errorf("call 3 should cycle back to first, got %q", got)
	}
}

func TestFauxEmptyResponses(t *testing.T) {
	f := &FauxProvider{}
	stream := f.Stream(ai.Model{ID: "m"}, ai.Context{}, nil)
	for range stream.C {
	}
	if stream.Result().StopReason != ai.StopReasonStop {
		t.Errorf("empty responses should still terminate with stop")
	}
}

func TestFauxContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	f := &FauxProvider{Responses: []FauxResponse{{
		Text:  "this should not fully stream",
		Delay: 10 * time.Millisecond,
	}}}
	stream := f.Stream(ai.Model{ID: "m"}, ai.Context{}, &ai.StreamOptions{Signal: ctx})

	var sawError bool
	for e := range stream.C {
		if e.Type == ai.EventError {
			sawError = true
		}
	}
	if !sawError {
		t.Error("expected error event on pre-cancelled context")
	}
	if stream.Result().StopReason != ai.StopReasonAborted {
		t.Errorf("stop reason = %q, want aborted", stream.Result().StopReason)
	}
}

func TestFauxRegisterDispatch(t *testing.T) {
	// End-to-end: register the faux provider and dispatch via ai.Stream.
	f := &FauxProvider{Responses: []FauxResponse{{Text: "dispatched"}}}
	ai.RegisterProvider(&ai.ApiProvider{
		Api:          ApiFaux,
		Stream:       f.Stream,
		StreamSimple: f.StreamSimple,
	})
	t.Cleanup(func() {
		// Reset not exposed from ai package; overwriting with nil is enough
		// for isolation between tests in other packages.
		ai.RegisterProvider(&ai.ApiProvider{Api: ApiFaux})
	})

	stream := ai.Stream(ai.Model{ID: "m", Api: ApiFaux}, ai.Context{}, nil)
	var got string
	for e := range stream.C {
		if e.Type == ai.EventTextDelta {
			got += e.Delta
		}
	}
	if got != "dispatched" {
		t.Errorf("got %q", got)
	}
}

func TestFauxStreamSimpleDelegates(t *testing.T) {
	f := &FauxProvider{Responses: []FauxResponse{{Text: "via simple"}}}
	stream := f.StreamSimple(ai.Model{ID: "m"}, ai.Context{}, &ai.SimpleStreamOptions{
		Reasoning: ai.ThinkingHigh,
	})
	var got string
	for e := range stream.C {
		if e.Type == ai.EventTextDelta {
			got += e.Delta
		}
	}
	if got != "via simple" {
		t.Errorf("got %q", got)
	}
}
