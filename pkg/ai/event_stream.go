package ai

import "sync"

// AssistantMessageEventStream carries streaming events from a provider to a
// consumer. Events are delivered over a buffered channel; once the stream
// terminates (done or error), the channel is closed and the final
// AssistantMessage is available via Result.
type AssistantMessageEventStream struct {
	// C is the consumer-facing read-only channel.
	C <-chan AssistantMessageEvent

	ch     chan AssistantMessageEvent
	result chan AssistantMessage
	done   chan struct{}
	once   sync.Once
}

// NewAssistantMessageEventStream creates an empty stream with a 64-event
// buffer.
func NewAssistantMessageEventStream() *AssistantMessageEventStream {
	ch := make(chan AssistantMessageEvent, 64)
	return &AssistantMessageEventStream{
		C:      ch,
		ch:     ch,
		result: make(chan AssistantMessage, 1),
		done:   make(chan struct{}),
	}
}

// Push sends an event to consumers. Safe to call from any goroutine and from
// multiple producers. Terminal events (EventDone, EventError) also trigger
// End. Pushes after the stream has ended are silently dropped.
func (s *AssistantMessageEventStream) Push(event AssistantMessageEvent) {
	select {
	case <-s.done:
		return
	default:
	}

	// Send safely: if End closes s.ch concurrently, a send would panic.
	// Recover so late producers don't crash the program.
	func() {
		defer func() { _ = recover() }()
		select {
		case <-s.done:
			return
		case s.ch <- event:
		}
	}()

	if event.Type == EventDone || event.Type == EventError {
		s.End(event)
	}
}

// End terminates the stream. It is idempotent and safe to call from any
// goroutine. If finalEvent carries a Message (done) or Error (error), it is
// delivered to waiters on Result.
func (s *AssistantMessageEventStream) End(finalEvent AssistantMessageEvent) {
	s.once.Do(func() {
		switch {
		case finalEvent.Type == EventDone && finalEvent.Message != nil:
			s.result <- *finalEvent.Message
		case finalEvent.Type == EventError && finalEvent.Error != nil:
			s.result <- *finalEvent.Error
		default:
			// No final message was attached; leave Result to block forever
			// unless a future caller has the result buffered. To avoid
			// deadlocks, publish a zero-value message.
			s.result <- AssistantMessage{}
		}
		close(s.done)
		close(s.ch)
	})
}

// Result blocks until the stream has terminated, then returns the final
// AssistantMessage. Safe to call from multiple goroutines: subsequent callers
// receive the same cached value.
func (s *AssistantMessageEventStream) Result() AssistantMessage {
	// First call drains the buffered result; subsequent calls re-publish it so
	// every waiter sees the value.
	msg, ok := <-s.result
	if !ok {
		return AssistantMessage{}
	}
	// Re-publish for the next reader.
	select {
	case s.result <- msg:
	default:
	}
	return msg
}

// Done returns a channel that is closed once the stream has terminated.
func (s *AssistantMessageEventStream) Done() <-chan struct{} {
	return s.done
}
