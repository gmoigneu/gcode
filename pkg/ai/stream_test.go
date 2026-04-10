package ai

import (
	"strings"
	"testing"
	"time"
)

func TestStreamDispatchesToProvider(t *testing.T) {
	resetRegistry(t)

	called := false
	RegisterProvider(&ApiProvider{
		Api: ApiAnthropicMessages,
		Stream: func(model Model, ctx Context, opts *StreamOptions) *AssistantMessageEventStream {
			called = true
			s := NewAssistantMessageEventStream()
			go func() {
				s.Push(AssistantMessageEvent{
					Type: EventDone,
					Message: &AssistantMessage{
						Model:      model.ID,
						StopReason: StopReasonStop,
					},
				})
			}()
			return s
		},
	})

	m := Model{ID: "m", Api: ApiAnthropicMessages}
	s := Stream(m, Context{}, &StreamOptions{})
	for range s.C {
	}
	if !called {
		t.Error("Stream did not call provider")
	}
	if s.Result().Model != "m" {
		t.Errorf("result = %+v", s.Result())
	}
}

func TestStreamSimpleDispatchesToProvider(t *testing.T) {
	resetRegistry(t)

	called := false
	RegisterProvider(&ApiProvider{
		Api: ApiOpenAICompletions,
		StreamSimple: func(model Model, ctx Context, opts *SimpleStreamOptions) *AssistantMessageEventStream {
			called = true
			s := NewAssistantMessageEventStream()
			go func() {
				s.Push(AssistantMessageEvent{
					Type:    EventDone,
					Message: &AssistantMessage{Model: model.ID},
				})
			}()
			return s
		},
	})

	m := Model{ID: "o", Api: ApiOpenAICompletions}
	s := StreamSimple(m, Context{}, &SimpleStreamOptions{Reasoning: ThinkingHigh})
	for range s.C {
	}
	if !called {
		t.Error("StreamSimple did not call provider")
	}
}

func TestStreamUnknownProviderEmitsError(t *testing.T) {
	resetRegistry(t)

	m := Model{ID: "m", Api: "missing-api"}
	s := Stream(m, Context{}, &StreamOptions{})

	var saw bool
	for e := range s.C {
		if e.Type == EventError {
			saw = true
			if e.Error == nil || !strings.Contains(e.Error.ErrorMessage, "missing-api") {
				t.Errorf("error event missing api name: %+v", e.Error)
			}
		}
	}
	if !saw {
		t.Error("expected error event for unknown api")
	}

	res := s.Result()
	if res.StopReason != StopReasonError {
		t.Errorf("result.StopReason = %q", res.StopReason)
	}
}

func TestStreamSimpleUnknownProviderEmitsError(t *testing.T) {
	resetRegistry(t)

	m := Model{ID: "m", Api: "missing-api"}
	s := StreamSimple(m, Context{}, &SimpleStreamOptions{})
	for range s.C {
	}
	if s.Result().StopReason != StopReasonError {
		t.Error("expected error stop reason")
	}
}

func TestStreamProviderWithoutStreamFunc(t *testing.T) {
	resetRegistry(t)
	RegisterProvider(&ApiProvider{Api: "nofunc"}) // no Stream/StreamSimple

	m := Model{Api: "nofunc"}
	s := Stream(m, Context{}, &StreamOptions{})
	for range s.C {
	}
	if s.Result().StopReason != StopReasonError {
		t.Error("expected error when provider has no Stream func")
	}

	s2 := StreamSimple(m, Context{}, &SimpleStreamOptions{})
	for range s2.C {
	}
	if s2.Result().StopReason != StopReasonError {
		t.Error("expected error when provider has no StreamSimple func")
	}
}

func TestCompleteReturnsResult(t *testing.T) {
	resetRegistry(t)

	RegisterProvider(&ApiProvider{
		Api: ApiAnthropicMessages,
		Stream: func(model Model, ctx Context, opts *StreamOptions) *AssistantMessageEventStream {
			s := NewAssistantMessageEventStream()
			go func() {
				s.Push(AssistantMessageEvent{Type: EventTextDelta, Delta: "hello"})
				s.Push(AssistantMessageEvent{
					Type:    EventDone,
					Message: &AssistantMessage{Model: "m", StopReason: StopReasonStop},
				})
			}()
			return s
		},
	})

	done := make(chan AssistantMessage, 1)
	go func() {
		done <- Complete(Model{Api: ApiAnthropicMessages}, Context{}, &StreamOptions{})
	}()

	select {
	case r := <-done:
		if r.Model != "m" {
			t.Errorf("complete result = %+v", r)
		}
	case <-time.After(time.Second):
		t.Fatal("Complete never returned")
	}
}

func TestCompleteSimpleReturnsResult(t *testing.T) {
	resetRegistry(t)

	RegisterProvider(&ApiProvider{
		Api: ApiOpenAICompletions,
		StreamSimple: func(model Model, ctx Context, opts *SimpleStreamOptions) *AssistantMessageEventStream {
			s := NewAssistantMessageEventStream()
			go func() {
				s.Push(AssistantMessageEvent{
					Type:    EventDone,
					Message: &AssistantMessage{Model: "x", StopReason: StopReasonStop},
				})
			}()
			return s
		},
	})

	r := CompleteSimple(Model{Api: ApiOpenAICompletions}, Context{}, &SimpleStreamOptions{})
	if r.Model != "x" {
		t.Errorf("result = %+v", r)
	}
}

func TestStreamNilOptionsDefaults(t *testing.T) {
	resetRegistry(t)
	RegisterProvider(&ApiProvider{
		Api: ApiAnthropicMessages,
		Stream: func(model Model, ctx Context, opts *StreamOptions) *AssistantMessageEventStream {
			if opts == nil {
				t.Error("Stream should not pass nil opts to providers")
			}
			s := NewAssistantMessageEventStream()
			go func() {
				s.Push(AssistantMessageEvent{
					Type:    EventDone,
					Message: &AssistantMessage{Model: "m"},
				})
			}()
			return s
		},
	})

	s := Stream(Model{Api: ApiAnthropicMessages}, Context{}, nil)
	for range s.C {
	}
}
