package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gmoigneu/gcode/pkg/ai"
)

// anthropicHTTPClient is the HTTP client used by StreamAnthropic. Tests may
// replace it with an httptest.Server client.
var anthropicHTTPClient = http.DefaultClient

func init() {
	ai.RegisterProvider(&ai.ApiProvider{
		Api:          ai.ApiAnthropicMessages,
		Stream:       StreamAnthropic,
		StreamSimple: StreamSimpleAnthropic,
	})
}

// StreamAnthropic issues a messages request and emits streaming events.
func StreamAnthropic(model ai.Model, ctx ai.Context, opts *ai.StreamOptions) *ai.AssistantMessageEventStream {
	stream := ai.NewAssistantMessageEventStream()
	go runAnthropicStream(stream, model, ctx, opts, nil)
	return stream
}

// StreamSimpleAnthropic mirrors StreamAnthropic but maps the requested
// reasoning level to Anthropic's thinking configuration.
func StreamSimpleAnthropic(model ai.Model, ctx ai.Context, opts *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
	stream := ai.NewAssistantMessageEventStream()
	go func() {
		var so *ai.StreamOptions
		if opts != nil {
			so = &opts.StreamOptions
		}
		var thinking *anthropicThinking
		var outputCfg *anthropicOutputCfg
		if opts != nil {
			thinking, outputCfg = anthropicThinkingFromLevel(opts.Reasoning, opts.ThinkingBudgets)
		}
		_ = outputCfg
		runAnthropicStream(stream, model, ctx, so, thinking)
	}()
	return stream
}

// anthropicThinkingFromLevel translates a ThinkingLevel into the request-
// level thinking config. The second return value is reserved for future
// adaptive-effort wiring.
func anthropicThinkingFromLevel(level ai.ThinkingLevel, budgets *ai.ThinkingBudgets) (*anthropicThinking, *anthropicOutputCfg) {
	if level == "" || level == ai.ThinkingOff {
		return nil, nil
	}
	budget := defaultBudget(level, budgets)
	return &anthropicThinking{Type: "enabled", BudgetTokens: budget}, nil
}

func defaultBudget(level ai.ThinkingLevel, b *ai.ThinkingBudgets) int {
	get := func(v, def int) int {
		if v > 0 {
			return v
		}
		return def
	}
	if b == nil {
		b = &ai.ThinkingBudgets{}
	}
	switch level {
	case ai.ThinkingMinimal:
		return get(b.Minimal, 1024)
	case ai.ThinkingLow:
		return get(b.Low, 2048)
	case ai.ThinkingMedium:
		return get(b.Medium, 8192)
	case ai.ThinkingHigh, ai.ThinkingXHigh:
		return get(b.High, 16384)
	}
	return 0
}

func runAnthropicStream(stream *ai.AssistantMessageEventStream, model ai.Model, c ai.Context, opts *ai.StreamOptions, thinking *anthropicThinking) {
	defer func() {
		if r := recover(); r != nil {
			emitAnthropicError(stream, model, fmt.Errorf("anthropic: panic: %v", r))
		}
	}()

	if opts == nil {
		opts = &ai.StreamOptions{}
	}

	retention := ai.ResolveCacheRetention(opts)
	cc := ai.GetCacheControl(model.BaseURL, retention)

	req := anthropicRequest{
		Model:     model.ID,
		MaxTokens: defaultAnthropicMaxTokens(model, opts),
		System:    convertSystemToAnthropic(c.SystemPrompt, cc),
		Messages:  convertMessagesToAnthropic(c.Messages, cc),
		Tools:     convertToolsToAnthropic(c.Tools),
		Stream:    true,
	}
	if opts.Temperature != nil {
		req.Temperature = opts.Temperature
	}
	if thinking != nil {
		req.Thinking = thinking
	}

	body, err := json.Marshal(req)
	if err != nil {
		emitAnthropicError(stream, model, fmt.Errorf("anthropic: marshal request: %w", err))
		return
	}

	signal := opts.Signal
	if signal == nil {
		signal = context.Background()
	}
	url := strings.TrimRight(model.BaseURL, "/") + "/messages"
	httpReq, err := http.NewRequestWithContext(signal, "POST", url, bytes.NewReader(body))
	if err != nil {
		emitAnthropicError(stream, model, fmt.Errorf("anthropic: build request: %w", err))
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	if opts.APIKey != "" {
		httpReq.Header.Set("x-api-key", opts.APIKey)
	}
	for k, v := range opts.Headers {
		httpReq.Header.Set(k, v)
	}
	for k, v := range model.Headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := anthropicHTTPClient.Do(httpReq)
	if err != nil {
		if signal.Err() != nil {
			emitAnthropicAborted(stream, model, signal.Err())
			return
		}
		emitAnthropicError(stream, model, fmt.Errorf("anthropic: http: %w", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(resp.Body)
		emitAnthropicError(stream, model, fmt.Errorf("anthropic: status %d: %s", resp.StatusCode, strings.TrimSpace(string(buf))))
		return
	}

	state := newAnthropicState(model)
	stream.Push(ai.AssistantMessageEvent{Type: ai.EventStart, Partial: state.partial})

	scanner := NewSSEScanner(resp.Body)
	for scanner.Scan() {
		if signal.Err() != nil {
			emitAnthropicAborted(stream, model, signal.Err())
			return
		}
		evt := scanner.Event()
		if evt.Data == "" {
			continue
		}
		var env anthropicStartEnvelope
		if err := json.Unmarshal([]byte(evt.Data), &env); err != nil {
			continue
		}
		state.handle(stream, evt, env)
	}
	if err := scanner.Err(); err != nil {
		emitAnthropicError(stream, model, fmt.Errorf("anthropic: sse: %w", err))
		return
	}

	state.finish(stream)
}

func defaultAnthropicMaxTokens(model ai.Model, opts *ai.StreamOptions) int {
	if opts != nil && opts.MaxTokens != nil {
		return *opts.MaxTokens
	}
	if model.MaxTokens > 0 {
		return model.MaxTokens
	}
	return 4096
}

// ----- streaming state machine -----

type anthropicState struct {
	partial    *ai.AssistantMessage
	model      ai.Model
	blocks     map[int]*anthropicBlock
	order      []int
	stopReason ai.StopReason
	usage      ai.Usage
}

type anthropicBlock struct {
	kind         string // text | thinking | tool_use | redacted_thinking
	contentIndex int
	text         string
	thinking     string
	signature    string
	toolCall     *ai.ToolCall
	partialJSON  string
}

func newAnthropicState(model ai.Model) *anthropicState {
	return &anthropicState{
		partial: &ai.AssistantMessage{
			Model:    model.ID,
			Api:      model.Api,
			Provider: model.Provider,
			Content:  []ai.Content{},
		},
		model:  model,
		blocks: map[int]*anthropicBlock{},
	}
}

func (s *anthropicState) handle(stream *ai.AssistantMessageEventStream, evt SSEEvent, env anthropicStartEnvelope) {
	typ := evt.Event
	if typ == "" {
		typ = env.Type
	}
	switch typ {
	case "message_start":
		if env.Message != nil && env.Message.Usage != nil {
			s.accumulateUsage(env.Message.Usage)
		}
	case "content_block_start":
		s.startBlock(stream, env.Index, env.Block)
	case "content_block_delta":
		s.blockDelta(stream, env.Index, env.Delta)
	case "content_block_stop":
		s.stopBlock(stream, env.Index)
	case "message_delta":
		if env.Delta != nil && env.Delta.StopReason != "" {
			s.stopReason = anthropicStopToAI(env.Delta.StopReason)
		}
		if env.Usage != nil {
			s.accumulateUsage(env.Usage)
		}
	case "message_stop":
		// nothing — caller invokes finish() after the scanner drains
	}
}

func (s *anthropicState) startBlock(stream *ai.AssistantMessageEventStream, index int, bs *anthropicBlockStart) {
	if bs == nil {
		return
	}
	b := &anthropicBlock{kind: bs.Type}
	switch bs.Type {
	case "text":
		tc := &ai.TextContent{Text: bs.Text}
		s.partial.Content = append(s.partial.Content, tc)
		b.contentIndex = len(s.partial.Content) - 1
		b.text = bs.Text
		stream.Push(ai.AssistantMessageEvent{
			Type:         ai.EventTextStart,
			ContentIndex: b.contentIndex,
			Partial:      s.partial,
		})
	case "thinking":
		tc := &ai.ThinkingContent{Thinking: bs.Text}
		s.partial.Content = append(s.partial.Content, tc)
		b.contentIndex = len(s.partial.Content) - 1
		b.thinking = bs.Text
		stream.Push(ai.AssistantMessageEvent{
			Type:         ai.EventThinkingStart,
			ContentIndex: b.contentIndex,
			Partial:      s.partial,
		})
	case "redacted_thinking":
		tc := &ai.ThinkingContent{Redacted: true}
		s.partial.Content = append(s.partial.Content, tc)
		b.contentIndex = len(s.partial.Content) - 1
	case "tool_use":
		tc := &ai.ToolCall{
			ID:        bs.ID,
			Name:      bs.Name,
			Arguments: map[string]any{},
		}
		s.partial.Content = append(s.partial.Content, tc)
		b.contentIndex = len(s.partial.Content) - 1
		b.toolCall = tc
		stream.Push(ai.AssistantMessageEvent{
			Type:         ai.EventToolCallStart,
			ContentIndex: b.contentIndex,
			ToolCall:     tc,
			Partial:      s.partial,
		})
	}
	s.blocks[index] = b
	s.order = append(s.order, index)
}

func (s *anthropicState) blockDelta(stream *ai.AssistantMessageEventStream, index int, delta *anthropicBlockDelta) {
	if delta == nil {
		return
	}
	b, ok := s.blocks[index]
	if !ok {
		return
	}
	switch delta.Type {
	case "text_delta":
		b.text += delta.Text
		if tc, ok := s.partial.Content[b.contentIndex].(*ai.TextContent); ok {
			tc.Text = b.text
		}
		stream.Push(ai.AssistantMessageEvent{
			Type:         ai.EventTextDelta,
			ContentIndex: b.contentIndex,
			Delta:        delta.Text,
			Partial:      s.partial,
		})
	case "thinking_delta":
		b.thinking += delta.Thinking
		if tc, ok := s.partial.Content[b.contentIndex].(*ai.ThinkingContent); ok {
			tc.Thinking = b.thinking
		}
		stream.Push(ai.AssistantMessageEvent{
			Type:         ai.EventThinkingDelta,
			ContentIndex: b.contentIndex,
			Delta:        delta.Thinking,
			Partial:      s.partial,
		})
	case "signature_delta":
		b.signature += delta.Text
		if tc, ok := s.partial.Content[b.contentIndex].(*ai.ThinkingContent); ok {
			tc.ThinkingSignature = b.signature
		}
	case "input_json_delta":
		b.partialJSON += delta.PartialJSON
		if b.toolCall != nil {
			b.toolCall.Arguments = ai.ParseStreamingJSON(b.partialJSON)
			stream.Push(ai.AssistantMessageEvent{
				Type:         ai.EventToolCallDelta,
				ContentIndex: b.contentIndex,
				ToolCall:     b.toolCall,
				Partial:      s.partial,
			})
		}
	}
}

func (s *anthropicState) stopBlock(stream *ai.AssistantMessageEventStream, index int) {
	b, ok := s.blocks[index]
	if !ok {
		return
	}
	switch b.kind {
	case "text":
		stream.Push(ai.AssistantMessageEvent{
			Type:         ai.EventTextEnd,
			ContentIndex: b.contentIndex,
			Content:      b.text,
			Partial:      s.partial,
		})
	case "thinking":
		stream.Push(ai.AssistantMessageEvent{
			Type:         ai.EventThinkingEnd,
			ContentIndex: b.contentIndex,
			Content:      b.thinking,
			Partial:      s.partial,
		})
	case "tool_use":
		if b.toolCall != nil {
			stream.Push(ai.AssistantMessageEvent{
				Type:         ai.EventToolCallEnd,
				ContentIndex: b.contentIndex,
				ToolCall:     b.toolCall,
				Partial:      s.partial,
			})
		}
	}
}

func (s *anthropicState) accumulateUsage(u *anthropicUsage) {
	s.usage.Input += u.InputTokens
	s.usage.Output += u.OutputTokens
	s.usage.CacheRead += u.CacheReadInputTokens
	s.usage.CacheWrite += u.CacheCreationInputTokens
	s.usage.TotalTokens = s.usage.Input + s.usage.Output + s.usage.CacheRead + s.usage.CacheWrite
}

func (s *anthropicState) finish(stream *ai.AssistantMessageEventStream) {
	reason := s.stopReason
	if reason == "" {
		if anyToolCalls(s.partial.Content) {
			reason = ai.StopReasonToolUse
		} else {
			reason = ai.StopReasonStop
		}
	}
	final := &ai.AssistantMessage{
		Model:      s.model.ID,
		Api:        s.model.Api,
		Provider:   s.model.Provider,
		Content:    s.partial.Content,
		StopReason: reason,
		Usage:      s.usage,
		Timestamp:  time.Now().UnixMilli(),
	}
	final.Usage.Cost = ai.CalculateCost(s.model, final.Usage)
	stream.Push(ai.AssistantMessageEvent{
		Type:    ai.EventDone,
		Reason:  reason,
		Message: final,
	})
}

func anyToolCalls(content []ai.Content) bool {
	for _, c := range content {
		if _, ok := c.(*ai.ToolCall); ok {
			return true
		}
	}
	return false
}

func anthropicStopToAI(s string) ai.StopReason {
	switch s {
	case "end_turn", "stop_sequence":
		return ai.StopReasonStop
	case "max_tokens":
		return ai.StopReasonLength
	case "tool_use":
		return ai.StopReasonToolUse
	}
	return ""
}

func emitAnthropicError(stream *ai.AssistantMessageEventStream, model ai.Model, err error) {
	stream.Push(ai.AssistantMessageEvent{
		Type:   ai.EventError,
		Reason: ai.StopReasonError,
		Error: &ai.AssistantMessage{
			Model:        model.ID,
			Api:          model.Api,
			Provider:     model.Provider,
			StopReason:   ai.StopReasonError,
			ErrorMessage: err.Error(),
			Timestamp:    time.Now().UnixMilli(),
		},
	})
}

func emitAnthropicAborted(stream *ai.AssistantMessageEventStream, model ai.Model, err error) {
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
