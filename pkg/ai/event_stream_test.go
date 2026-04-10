package ai

import (
	"sync"
	"testing"
	"time"
)

func TestEventStreamPushReceive(t *testing.T) {
	s := NewAssistantMessageEventStream()
	go func() {
		s.Push(AssistantMessageEvent{Type: EventTextDelta, Delta: "hi"})
		s.Push(AssistantMessageEvent{
			Type:    EventDone,
			Reason:  StopReasonStop,
			Message: &AssistantMessage{Model: "m", StopReason: StopReasonStop},
		})
	}()

	var got []AssistantMessageEvent
	for e := range s.C {
		got = append(got, e)
	}
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2", len(got))
	}
	if got[0].Type != EventTextDelta || got[0].Delta != "hi" {
		t.Errorf("first: %+v", got[0])
	}
	if got[1].Type != EventDone {
		t.Errorf("second: %+v", got[1])
	}
}

func TestEventStreamResultOnDone(t *testing.T) {
	s := NewAssistantMessageEventStream()
	want := AssistantMessage{Model: "m", StopReason: StopReasonStop, Timestamp: 42}
	go func() {
		s.Push(AssistantMessageEvent{Type: EventDone, Message: &want})
	}()

	// Drain the channel to let End proceed.
	for range s.C {
	}

	got := s.Result()
	if got.Model != "m" || got.StopReason != StopReasonStop || got.Timestamp != 42 {
		t.Errorf("result = %+v", got)
	}
}

func TestEventStreamResultOnError(t *testing.T) {
	s := NewAssistantMessageEventStream()
	errMsg := &AssistantMessage{
		Model:        "m",
		StopReason:   StopReasonError,
		ErrorMessage: "boom",
	}
	go func() {
		s.Push(AssistantMessageEvent{Type: EventError, Reason: StopReasonError, Error: errMsg})
	}()

	for range s.C {
	}

	got := s.Result()
	if got.ErrorMessage != "boom" {
		t.Errorf("result = %+v", got)
	}
}

func TestEventStreamResultBlocks(t *testing.T) {
	s := NewAssistantMessageEventStream()

	done := make(chan AssistantMessage, 1)
	go func() { done <- s.Result() }()

	select {
	case <-done:
		t.Fatal("Result returned before terminal event")
	case <-time.After(20 * time.Millisecond):
	}

	go func() {
		s.Push(AssistantMessageEvent{
			Type:    EventDone,
			Message: &AssistantMessage{Model: "m"},
		})
	}()
	for range s.C {
	}

	select {
	case m := <-done:
		if m.Model != "m" {
			t.Errorf("result = %+v", m)
		}
	case <-time.After(time.Second):
		t.Fatal("Result did not return after done")
	}
}

func TestEventStreamDoneChannel(t *testing.T) {
	s := NewAssistantMessageEventStream()

	select {
	case <-s.Done():
		t.Fatal("Done closed before any event")
	default:
	}

	s.End(AssistantMessageEvent{
		Type:    EventDone,
		Message: &AssistantMessage{Model: "m"},
	})

	select {
	case <-s.Done():
	case <-time.After(time.Second):
		t.Fatal("Done did not close after End")
	}
}

func TestEventStreamEndIdempotent(t *testing.T) {
	s := NewAssistantMessageEventStream()
	final := AssistantMessageEvent{
		Type:    EventDone,
		Message: &AssistantMessage{Model: "m"},
	}
	s.End(final)
	s.End(final) // should not panic or double-close
	s.End(final)

	if got := s.Result(); got.Model != "m" {
		t.Errorf("result = %+v", got)
	}
}

func TestEventStreamPushAfterEndNoPanic(t *testing.T) {
	s := NewAssistantMessageEventStream()
	s.End(AssistantMessageEvent{
		Type:    EventDone,
		Message: &AssistantMessage{Model: "m"},
	})
	// This must not panic.
	s.Push(AssistantMessageEvent{Type: EventTextDelta, Delta: "late"})
}

func TestEventStreamConcurrentPushers(t *testing.T) {
	s := NewAssistantMessageEventStream()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				s.Push(AssistantMessageEvent{Type: EventTextDelta, Delta: "x"})
			}
		}(i)
	}

	// consume events in the background
	consumed := make(chan int, 1)
	go func() {
		n := 0
		for range s.C {
			n++
		}
		consumed <- n
	}()

	wg.Wait()
	s.Push(AssistantMessageEvent{
		Type:    EventDone,
		Message: &AssistantMessage{Model: "m"},
	})

	select {
	case n := <-consumed:
		// At least the done event should be counted. Concurrent pushers
		// may be dropped if the buffer fills after End, but nothing should panic.
		if n < 1 {
			t.Errorf("consumer saw %d events", n)
		}
	case <-time.After(time.Second):
		t.Fatal("consumer never finished")
	}
}

func TestEventStreamBufferedSend(t *testing.T) {
	s := NewAssistantMessageEventStream()
	// Without a consumer, a non-terminal Push should not block up to the buffer size.
	for i := 0; i < 64; i++ {
		s.Push(AssistantMessageEvent{Type: EventTextDelta, Delta: "x"})
	}
	// Now drain and terminate.
	go func() {
		s.Push(AssistantMessageEvent{
			Type:    EventDone,
			Message: &AssistantMessage{Model: "m"},
		})
	}()
	for range s.C {
	}
	if s.Result().Model != "m" {
		t.Fatal("terminal message lost")
	}
}
