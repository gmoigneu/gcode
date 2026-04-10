package ai

import (
	"fmt"
	"time"
)

// Stream kicks off a raw streaming request against the provider registered
// for model.Api. If no provider is registered (or the provider has no Stream
// implementation) the returned stream immediately emits a single error event.
func Stream(model Model, ctx Context, opts *StreamOptions) *AssistantMessageEventStream {
	if opts == nil {
		opts = &StreamOptions{}
	}
	p, ok := GetProvider(model.Api)
	if !ok {
		return errorStream(model, fmt.Errorf("ai: no provider registered for api %q", model.Api))
	}
	if p.Stream == nil {
		return errorStream(model, fmt.Errorf("ai: provider %q has no Stream implementation", model.Api))
	}
	return p.Stream(model, ctx, opts)
}

// StreamSimple kicks off a reasoning-aware streaming request.
func StreamSimple(model Model, ctx Context, opts *SimpleStreamOptions) *AssistantMessageEventStream {
	if opts == nil {
		opts = &SimpleStreamOptions{}
	}
	p, ok := GetProvider(model.Api)
	if !ok {
		return errorStream(model, fmt.Errorf("ai: no provider registered for api %q", model.Api))
	}
	if p.StreamSimple == nil {
		return errorStream(model, fmt.Errorf("ai: provider %q has no StreamSimple implementation", model.Api))
	}
	return p.StreamSimple(model, ctx, opts)
}

// Complete runs Stream and blocks on Result, returning the final
// AssistantMessage.
func Complete(model Model, ctx Context, opts *StreamOptions) AssistantMessage {
	s := Stream(model, ctx, opts)
	// Drain events so End actually fires. The buffer is 64 — if the producer
	// pushes more than that without a consumer, it would block.
	for range s.C {
	}
	return s.Result()
}

// CompleteSimple runs StreamSimple and blocks on Result.
func CompleteSimple(model Model, ctx Context, opts *SimpleStreamOptions) AssistantMessage {
	s := StreamSimple(model, ctx, opts)
	for range s.C {
	}
	return s.Result()
}

// errorStream returns a stream that immediately emits an EventError event
// carrying err, then terminates.
func errorStream(model Model, err error) *AssistantMessageEventStream {
	stream := NewAssistantMessageEventStream()
	go func() {
		errMsg := &AssistantMessage{
			Api:          model.Api,
			Provider:     model.Provider,
			Model:        model.ID,
			StopReason:   StopReasonError,
			ErrorMessage: err.Error(),
			Timestamp:    time.Now().UnixMilli(),
		}
		stream.Push(AssistantMessageEvent{
			Type:   EventError,
			Reason: StopReasonError,
			Error:  errMsg,
		})
	}()
	return stream
}
