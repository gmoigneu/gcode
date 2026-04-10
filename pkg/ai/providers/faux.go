package providers

import (
	"context"
	"sync"
	"time"

	"github.com/gmoigneu/gcode/pkg/ai"
)

// ApiFaux is the Api identifier reserved for the in-memory FauxProvider.
// Register it with ai.RegisterProvider to use Stream/Complete with canned
// responses.
const ApiFaux ai.Api = "faux"

// FauxResponse is one canned reply from a FauxProvider. An empty response
// still terminates with EventDone.
type FauxResponse struct {
	// Thinking, Text and ToolCalls are emitted in that order.
	Thinking  string
	Text      string
	ToolCalls []ai.ToolCall
	// Delay is applied between each streamed rune (for text and thinking).
	Delay time.Duration
	// StopReason overrides the default terminal reason. If empty, it
	// defaults to StopReasonToolUse when ToolCalls is non-empty, otherwise
	// StopReasonStop.
	StopReason ai.StopReason
}

// FauxProvider serves canned responses in order, cycling when exhausted.
// It is safe to share across goroutines.
type FauxProvider struct {
	Responses []FauxResponse

	mu        sync.Mutex
	callIndex int
}

// next returns the next canned response and advances the cursor. When the
// Responses slice is empty it returns a zero-value response.
func (f *FauxProvider) next() FauxResponse {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.Responses) == 0 {
		return FauxResponse{}
	}
	r := f.Responses[f.callIndex%len(f.Responses)]
	f.callIndex++
	return r
}

// Stream emits a FauxResponse as a sequence of ai.AssistantMessageEvents.
func (f *FauxProvider) Stream(model ai.Model, _ ai.Context, opts *ai.StreamOptions) *ai.AssistantMessageEventStream {
	stream := ai.NewAssistantMessageEventStream()
	resp := f.next()

	var signal context.Context
	if opts != nil {
		signal = opts.Signal
	}

	go func() {
		emitFaux(stream, model, resp, signal)
	}()
	return stream
}

// StreamSimple delegates to Stream with the underlying StreamOptions. The
// reasoning fields are ignored by the faux provider.
func (f *FauxProvider) StreamSimple(model ai.Model, c ai.Context, opts *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
	var so *ai.StreamOptions
	if opts != nil {
		so = &opts.StreamOptions
	}
	return f.Stream(model, c, so)
}

// emitFaux pushes the events for one canned response. It honours a cancellable
// context by emitting an error event with StopReasonAborted.
func emitFaux(stream *ai.AssistantMessageEventStream, model ai.Model, resp FauxResponse, signal context.Context) {
	partial := &ai.AssistantMessage{
		Model:    model.ID,
		Api:      model.Api,
		Provider: model.Provider,
		Content:  []ai.Content{},
	}

	pushErr := func(err error) {
		stream.Push(ai.AssistantMessageEvent{
			Type:   ai.EventError,
			Reason: ai.StopReasonAborted,
			Error: &ai.AssistantMessage{
				Model:        model.ID,
				Api:          model.Api,
				Provider:     model.Provider,
				StopReason:   ai.StopReasonAborted,
				ErrorMessage: err.Error(),
				Timestamp:    time.Now().UnixMilli(),
			},
		})
	}

	checkCancel := func() error {
		if signal == nil {
			return nil
		}
		select {
		case <-signal.Done():
			return signal.Err()
		default:
			return nil
		}
	}

	stream.Push(ai.AssistantMessageEvent{Type: ai.EventStart, Partial: partial})

	idx := 0

	if resp.Thinking != "" {
		if err := checkCancel(); err != nil {
			pushErr(err)
			return
		}
		tc := &ai.ThinkingContent{}
		partial.Content = append(partial.Content, tc)
		stream.Push(ai.AssistantMessageEvent{Type: ai.EventThinkingStart, ContentIndex: idx, Partial: partial})
		for _, r := range resp.Thinking {
			if err := checkCancel(); err != nil {
				pushErr(err)
				return
			}
			tc.Thinking += string(r)
			stream.Push(ai.AssistantMessageEvent{
				Type:         ai.EventThinkingDelta,
				ContentIndex: idx,
				Delta:        string(r),
				Partial:      partial,
			})
			if resp.Delay > 0 {
				time.Sleep(resp.Delay)
			}
		}
		stream.Push(ai.AssistantMessageEvent{
			Type:         ai.EventThinkingEnd,
			ContentIndex: idx,
			Content:      tc.Thinking,
			Partial:      partial,
		})
		idx++
	}

	if resp.Text != "" {
		if err := checkCancel(); err != nil {
			pushErr(err)
			return
		}
		tc := &ai.TextContent{}
		partial.Content = append(partial.Content, tc)
		stream.Push(ai.AssistantMessageEvent{Type: ai.EventTextStart, ContentIndex: idx, Partial: partial})
		for _, r := range resp.Text {
			if err := checkCancel(); err != nil {
				pushErr(err)
				return
			}
			tc.Text += string(r)
			stream.Push(ai.AssistantMessageEvent{
				Type:         ai.EventTextDelta,
				ContentIndex: idx,
				Delta:        string(r),
				Partial:      partial,
			})
			if resp.Delay > 0 {
				time.Sleep(resp.Delay)
			}
		}
		stream.Push(ai.AssistantMessageEvent{
			Type:         ai.EventTextEnd,
			ContentIndex: idx,
			Content:      tc.Text,
			Partial:      partial,
		})
		idx++
	}

	for i := range resp.ToolCalls {
		if err := checkCancel(); err != nil {
			pushErr(err)
			return
		}
		cpy := resp.ToolCalls[i]
		partial.Content = append(partial.Content, &cpy)
		stream.Push(ai.AssistantMessageEvent{
			Type:         ai.EventToolCallStart,
			ContentIndex: idx,
			ToolCall:     &cpy,
			Partial:      partial,
		})
		stream.Push(ai.AssistantMessageEvent{
			Type:         ai.EventToolCallEnd,
			ContentIndex: idx,
			ToolCall:     &cpy,
			Partial:      partial,
		})
		idx++
	}

	stopReason := resp.StopReason
	if stopReason == "" {
		if len(resp.ToolCalls) > 0 {
			stopReason = ai.StopReasonToolUse
		} else {
			stopReason = ai.StopReasonStop
		}
	}

	final := &ai.AssistantMessage{
		Model:      model.ID,
		Api:        model.Api,
		Provider:   model.Provider,
		Content:    partial.Content,
		StopReason: stopReason,
		Timestamp:  time.Now().UnixMilli(),
	}

	stream.Push(ai.AssistantMessageEvent{
		Type:    ai.EventDone,
		Reason:  stopReason,
		Message: final,
	})
}
