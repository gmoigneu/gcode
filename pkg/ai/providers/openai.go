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

// init registers the OpenAI-compat provider with the ai package. The
// registration is idempotent: later calls (for example, from tests) will
// replace the binding.
func init() {
	ai.RegisterProvider(&ai.ApiProvider{
		Api:          ai.ApiOpenAICompletions,
		Stream:       StreamOpenAI,
		StreamSimple: StreamSimpleOpenAI,
	})
}

// openAIHTTPClient is the HTTP client used by the provider. Tests may
// override it to inject httptest.Server.Client().
var openAIHTTPClient = http.DefaultClient

// StreamOpenAI issues a chat/completions request and emits events as the
// SSE stream arrives.
func StreamOpenAI(model ai.Model, ctx ai.Context, opts *ai.StreamOptions) *ai.AssistantMessageEventStream {
	stream := ai.NewAssistantMessageEventStream()
	go func() {
		runOpenAIStream(stream, model, ctx, opts, "")
	}()
	return stream
}

// StreamSimpleOpenAI wraps StreamOpenAI with a reasoning_effort mapping
// derived from the simple options.
func StreamSimpleOpenAI(model ai.Model, ctx ai.Context, opts *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
	stream := ai.NewAssistantMessageEventStream()
	go func() {
		var so *ai.StreamOptions
		if opts != nil {
			so = &opts.StreamOptions
		}
		effort := ""
		if opts != nil {
			effort = reasoningEffortFor(opts.Reasoning, model)
		}
		runOpenAIStream(stream, model, ctx, so, effort)
	}()
	return stream
}

// reasoningEffortFor maps a ThinkingLevel to the provider's reasoning_effort
// string. If the model has a custom map it takes precedence.
func reasoningEffortFor(level ai.ThinkingLevel, model ai.Model) string {
	if level == "" || level == ai.ThinkingOff {
		return ""
	}
	if model.Compat != nil && len(model.Compat.ReasoningEffortMap) > 0 {
		if v, ok := model.Compat.ReasoningEffortMap[level]; ok {
			return v
		}
	}
	// Default mapping: the OpenAI-supplied buckets are minimal/low/medium/high.
	switch level {
	case ai.ThinkingMinimal:
		return "minimal"
	case ai.ThinkingLow:
		return "low"
	case ai.ThinkingMedium:
		return "medium"
	case ai.ThinkingHigh, ai.ThinkingXHigh:
		return "high"
	}
	return ""
}

func runOpenAIStream(stream *ai.AssistantMessageEventStream, model ai.Model, c ai.Context, opts *ai.StreamOptions, reasoningEffort string) {
	defer func() {
		if r := recover(); r != nil {
			emitOpenAIError(stream, model, fmt.Errorf("openai: panic: %v", r))
		}
	}()

	if opts == nil {
		opts = &ai.StreamOptions{}
	}

	compat := GetCompat(model)

	req := openAIRequest{
		Model:           model.ID,
		Messages:        convertMessagesToOpenAI(c.SystemPrompt, c.Messages, compat),
		Stream:          true,
		Temperature:     opts.Temperature,
		MaxTokens:       opts.MaxTokens,
		MaxTokensField:  compat.MaxTokensField,
		Tools:           convertToolsToOpenAI(c.Tools, compat),
		ReasoningEffort: reasoningEffort,
	}
	if compat.SupportsUsageInStreaming {
		req.StreamOptions = &openAIStreamOpts{IncludeUsage: true}
	}

	body, err := json.Marshal(req)
	if err != nil {
		emitOpenAIError(stream, model, fmt.Errorf("openai: marshal request: %w", err))
		return
	}

	signal := opts.Signal
	if signal == nil {
		signal = context.Background()
	}

	url := strings.TrimRight(model.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(signal, "POST", url, bytes.NewReader(body))
	if err != nil {
		emitOpenAIError(stream, model, fmt.Errorf("openai: build http request: %w", err))
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if opts.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+opts.APIKey)
	}
	for k, v := range opts.Headers {
		httpReq.Header.Set(k, v)
	}
	for k, v := range model.Headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := openAIHTTPClient.Do(httpReq)
	if err != nil {
		if signal.Err() != nil {
			emitOpenAIAborted(stream, model, signal.Err())
			return
		}
		emitOpenAIError(stream, model, fmt.Errorf("openai: http: %w", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(resp.Body)
		emitOpenAIError(stream, model, fmt.Errorf("openai: status %d: %s", resp.StatusCode, strings.TrimSpace(string(buf))))
		return
	}

	state := newOpenAIChunkState(model)
	stream.Push(ai.AssistantMessageEvent{Type: ai.EventStart, Partial: state.partial})

	scanner := NewSSEScanner(resp.Body)
	for scanner.Scan() {
		if signal.Err() != nil {
			emitOpenAIAborted(stream, model, signal.Err())
			return
		}
		evt := scanner.Event()
		if evt.Data == "" || evt.Data == "[DONE]" {
			if evt.Data == "[DONE]" {
				break
			}
			continue
		}
		var chunk openAIChunk
		if err := json.Unmarshal([]byte(evt.Data), &chunk); err != nil {
			continue
		}
		state.handleChunk(stream, &chunk)
	}
	if err := scanner.Err(); err != nil {
		emitOpenAIError(stream, model, fmt.Errorf("openai: sse: %w", err))
		return
	}

	state.finish(stream)
}

// openAIChunkState tracks the in-progress assistant message as SSE chunks
// arrive.
type openAIChunkState struct {
	partial       *ai.AssistantMessage
	model         ai.Model
	currentBlock  ai.Content // current text/thinking block
	currentIndex  int
	toolCallsByIx map[int]*toolCallProgress
	orderedTools  []int
	stopReason    ai.StopReason
	usage         *ai.Usage
}

type toolCallProgress struct {
	id           string
	name         string
	argsBuf      string
	contentIndex int
	call         *ai.ToolCall
	started      bool
}

func newOpenAIChunkState(model ai.Model) *openAIChunkState {
	return &openAIChunkState{
		partial: &ai.AssistantMessage{
			Model:    model.ID,
			Api:      model.Api,
			Provider: model.Provider,
			Content:  []ai.Content{},
		},
		model:         model,
		toolCallsByIx: map[int]*toolCallProgress{},
	}
}

func (s *openAIChunkState) handleChunk(stream *ai.AssistantMessageEventStream, chunk *openAIChunk) {
	if chunk.Usage != nil {
		u := openAIUsageToAI(chunk.Usage, s.model)
		s.usage = &u
	}
	for i := range chunk.Choices {
		s.handleChoice(stream, &chunk.Choices[i])
	}
}

func (s *openAIChunkState) handleChoice(stream *ai.AssistantMessageEventStream, choice *openAIChoice) {
	d := choice.Delta

	// Text content.
	if d.Content != "" {
		s.appendText(stream, d.Content)
	}
	// Reasoning content (thinking) — accept any of the known field names.
	if r := firstNonEmpty(d.ReasoningContent, d.Reasoning, d.ReasoningText); r != "" {
		s.appendThinking(stream, r)
	}
	// Tool call deltas.
	for _, tc := range d.ToolCalls {
		s.handleToolCallDelta(stream, tc)
	}
	// Finish reason.
	if choice.FinishReason != "" {
		s.stopReason = openAIFinishToStopReason(choice.FinishReason)
	}
}

func (s *openAIChunkState) appendText(stream *ai.AssistantMessageEventStream, delta string) {
	tc, ok := s.currentBlock.(*ai.TextContent)
	if !ok {
		s.finishCurrentBlock(stream)
		tc = &ai.TextContent{}
		s.partial.Content = append(s.partial.Content, tc)
		s.currentIndex = len(s.partial.Content) - 1
		s.currentBlock = tc
		stream.Push(ai.AssistantMessageEvent{
			Type:         ai.EventTextStart,
			ContentIndex: s.currentIndex,
			Partial:      s.partial,
		})
	}
	tc.Text += delta
	stream.Push(ai.AssistantMessageEvent{
		Type:         ai.EventTextDelta,
		ContentIndex: s.currentIndex,
		Delta:        delta,
		Partial:      s.partial,
	})
}

func (s *openAIChunkState) appendThinking(stream *ai.AssistantMessageEventStream, delta string) {
	tc, ok := s.currentBlock.(*ai.ThinkingContent)
	if !ok {
		s.finishCurrentBlock(stream)
		tc = &ai.ThinkingContent{}
		s.partial.Content = append(s.partial.Content, tc)
		s.currentIndex = len(s.partial.Content) - 1
		s.currentBlock = tc
		stream.Push(ai.AssistantMessageEvent{
			Type:         ai.EventThinkingStart,
			ContentIndex: s.currentIndex,
			Partial:      s.partial,
		})
	}
	tc.Thinking += delta
	stream.Push(ai.AssistantMessageEvent{
		Type:         ai.EventThinkingDelta,
		ContentIndex: s.currentIndex,
		Delta:        delta,
		Partial:      s.partial,
	})
}

func (s *openAIChunkState) finishCurrentBlock(stream *ai.AssistantMessageEventStream) {
	switch b := s.currentBlock.(type) {
	case *ai.TextContent:
		stream.Push(ai.AssistantMessageEvent{
			Type:         ai.EventTextEnd,
			ContentIndex: s.currentIndex,
			Content:      b.Text,
			Partial:      s.partial,
		})
	case *ai.ThinkingContent:
		stream.Push(ai.AssistantMessageEvent{
			Type:         ai.EventThinkingEnd,
			ContentIndex: s.currentIndex,
			Content:      b.Thinking,
			Partial:      s.partial,
		})
	}
	s.currentBlock = nil
}

func (s *openAIChunkState) handleToolCallDelta(stream *ai.AssistantMessageEventStream, tc openAIToolCallDelta) {
	idx := 0
	if tc.Index != nil {
		idx = *tc.Index
	}
	prog, ok := s.toolCallsByIx[idx]
	if !ok {
		s.finishCurrentBlock(stream)
		prog = &toolCallProgress{
			call: &ai.ToolCall{Arguments: map[string]any{}},
		}
		s.toolCallsByIx[idx] = prog
		s.orderedTools = append(s.orderedTools, idx)
	}
	if tc.ID != "" {
		prog.id = tc.ID
		prog.call.ID = tc.ID
	}
	if tc.Function != nil {
		if tc.Function.Name != "" {
			prog.name = tc.Function.Name
			prog.call.Name = tc.Function.Name
		}
		if tc.Function.Arguments != "" {
			prog.argsBuf += tc.Function.Arguments
			prog.call.Arguments = ai.ParseStreamingJSON(prog.argsBuf)
		}
	}
	if !prog.started && prog.call.Name != "" {
		prog.started = true
		s.partial.Content = append(s.partial.Content, prog.call)
		prog.contentIndex = len(s.partial.Content) - 1
		stream.Push(ai.AssistantMessageEvent{
			Type:         ai.EventToolCallStart,
			ContentIndex: prog.contentIndex,
			ToolCall:     prog.call,
			Partial:      s.partial,
		})
	}
	if prog.started {
		stream.Push(ai.AssistantMessageEvent{
			Type:         ai.EventToolCallDelta,
			ContentIndex: prog.contentIndex,
			ToolCall:     prog.call,
			Partial:      s.partial,
		})
	}
}

func (s *openAIChunkState) finish(stream *ai.AssistantMessageEventStream) {
	s.finishCurrentBlock(stream)
	for _, idx := range s.orderedTools {
		prog := s.toolCallsByIx[idx]
		if !prog.started {
			continue
		}
		stream.Push(ai.AssistantMessageEvent{
			Type:         ai.EventToolCallEnd,
			ContentIndex: prog.contentIndex,
			ToolCall:     prog.call,
			Partial:      s.partial,
		})
	}

	reason := s.stopReason
	if reason == "" {
		// Default to tool_use if any tool calls were emitted, otherwise stop.
		if len(s.toolCallsByIx) > 0 {
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
		Timestamp:  time.Now().UnixMilli(),
	}
	if s.usage != nil {
		final.Usage = *s.usage
	}
	final.Usage.Cost = ai.CalculateCost(s.model, final.Usage)

	stream.Push(ai.AssistantMessageEvent{
		Type:    ai.EventDone,
		Reason:  reason,
		Message: final,
	})
}

func openAIFinishToStopReason(fr string) ai.StopReason {
	switch fr {
	case "stop":
		return ai.StopReasonStop
	case "length":
		return ai.StopReasonLength
	case "tool_calls":
		return ai.StopReasonToolUse
	case "content_filter":
		return ai.StopReasonError
	}
	return ""
}

func openAIUsageToAI(u *openAIUsage, model ai.Model) ai.Usage {
	out := ai.Usage{
		Input:       u.PromptTokens,
		Output:      u.CompletionTokens,
		TotalTokens: u.TotalTokens,
	}
	if u.PromptTokensDetails != nil {
		input, read, write := ai.NormalizeCacheUsage(
			u.PromptTokens,
			u.PromptTokensDetails.CachedTokens,
			u.PromptTokensDetails.CacheWriteTokens,
		)
		out.Input = input
		out.CacheRead = read
		out.CacheWrite = write
	}
	out.Cost = ai.CalculateCost(model, out)
	return out
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func emitOpenAIError(stream *ai.AssistantMessageEventStream, model ai.Model, err error) {
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

func emitOpenAIAborted(stream *ai.AssistantMessageEventStream, model ai.Model, err error) {
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
