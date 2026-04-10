package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gmoigneu/gcode/pkg/ai"
)

// googleHTTPClient is the HTTP client used by StreamGoogle.
var googleHTTPClient = http.DefaultClient

func init() {
	ai.RegisterProvider(&ai.ApiProvider{
		Api:          ai.ApiGoogleGemini,
		Stream:       StreamGoogle,
		StreamSimple: StreamSimpleGoogle,
	})
}

// StreamGoogle starts a streamGenerateContent request and emits events.
func StreamGoogle(model ai.Model, ctx ai.Context, opts *ai.StreamOptions) *ai.AssistantMessageEventStream {
	stream := ai.NewAssistantMessageEventStream()
	go runGoogleStream(stream, model, ctx, opts, nil)
	return stream
}

// StreamSimpleGoogle honours the ThinkingLevel by setting thinking_config.
func StreamSimpleGoogle(model ai.Model, ctx ai.Context, opts *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
	stream := ai.NewAssistantMessageEventStream()
	go func() {
		var so *ai.StreamOptions
		if opts != nil {
			so = &opts.StreamOptions
		}
		var cfg *googleThinkingCfg
		if opts != nil {
			cfg = googleThinkingFromLevel(opts.Reasoning, opts.ThinkingBudgets)
		}
		runGoogleStream(stream, model, ctx, so, cfg)
	}()
	return stream
}

func googleThinkingFromLevel(level ai.ThinkingLevel, budgets *ai.ThinkingBudgets) *googleThinkingCfg {
	if level == "" || level == ai.ThinkingOff {
		return nil
	}
	return &googleThinkingCfg{
		ThinkingBudget:  defaultBudget(level, budgets),
		IncludeThoughts: true,
	}
}

func runGoogleStream(stream *ai.AssistantMessageEventStream, model ai.Model, c ai.Context, opts *ai.StreamOptions, thinking *googleThinkingCfg) {
	defer func() {
		if r := recover(); r != nil {
			emitGoogleError(stream, model, fmt.Errorf("google: panic: %v", r))
		}
	}()

	if opts == nil {
		opts = &ai.StreamOptions{}
	}

	req := googleRequest{
		Contents:          convertMessagesToGoogle(c.Messages),
		SystemInstruction: convertSystemToGoogle(c.SystemPrompt),
		Tools:             convertToolsToGoogle(c.Tools),
		ThinkingConfig:    thinking,
	}
	if opts.Temperature != nil || opts.MaxTokens != nil {
		req.GenerationConfig = &googleGenConfig{
			Temperature:     opts.Temperature,
			MaxOutputTokens: opts.MaxTokens,
		}
	}

	body, err := json.Marshal(req)
	if err != nil {
		emitGoogleError(stream, model, fmt.Errorf("google: marshal: %w", err))
		return
	}

	signal := opts.Signal
	if signal == nil {
		signal = context.Background()
	}

	endpoint, err := buildGoogleURL(model, opts.APIKey)
	if err != nil {
		emitGoogleError(stream, model, err)
		return
	}

	httpReq, err := http.NewRequestWithContext(signal, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		emitGoogleError(stream, model, fmt.Errorf("google: build request: %w", err))
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	for k, v := range opts.Headers {
		httpReq.Header.Set(k, v)
	}
	for k, v := range model.Headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := googleHTTPClient.Do(httpReq)
	if err != nil {
		if signal.Err() != nil {
			emitGoogleAborted(stream, model, signal.Err())
			return
		}
		emitGoogleError(stream, model, fmt.Errorf("google: http: %w", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(resp.Body)
		emitGoogleError(stream, model, fmt.Errorf("google: status %d: %s", resp.StatusCode, strings.TrimSpace(string(buf))))
		return
	}

	state := newGoogleState(model)
	stream.Push(ai.AssistantMessageEvent{Type: ai.EventStart, Partial: state.partial})

	scanner := NewSSEScanner(resp.Body)
	for scanner.Scan() {
		if signal.Err() != nil {
			emitGoogleAborted(stream, model, signal.Err())
			return
		}
		evt := scanner.Event()
		if evt.Data == "" {
			continue
		}
		var chunk googleStreamChunk
		if err := json.Unmarshal([]byte(evt.Data), &chunk); err != nil {
			continue
		}
		state.handle(stream, &chunk)
	}
	if err := scanner.Err(); err != nil {
		emitGoogleError(stream, model, fmt.Errorf("google: sse: %w", err))
		return
	}

	state.finish(stream)
}

func buildGoogleURL(model ai.Model, apiKey string) (string, error) {
	if model.BaseURL == "" {
		return "", fmt.Errorf("google: model BaseURL is required")
	}
	base := strings.TrimRight(model.BaseURL, "/")
	raw := fmt.Sprintf("%s/models/%s:streamGenerateContent", base, model.ID)

	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("google: bad base URL: %w", err)
	}
	q := u.Query()
	q.Set("alt", "sse")
	if apiKey != "" {
		q.Set("key", apiKey)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// ----- streaming state -----

type googleState struct {
	partial     *ai.AssistantMessage
	model       ai.Model
	textIndex   int
	textBlock   *ai.TextContent
	stopReason  ai.StopReason
	usage       ai.Usage
	toolCounter int
}

func newGoogleState(model ai.Model) *googleState {
	return &googleState{
		partial: &ai.AssistantMessage{
			Model:    model.ID,
			Api:      model.Api,
			Provider: model.Provider,
			Content:  []ai.Content{},
		},
		model:     model,
		textIndex: -1,
	}
}

func (s *googleState) handle(stream *ai.AssistantMessageEventStream, chunk *googleStreamChunk) {
	if chunk.UsageMetadata != nil {
		u := chunk.UsageMetadata
		s.usage.Input = u.PromptTokenCount
		s.usage.Output = u.CandidatesTokenCount
		s.usage.TotalTokens = u.TotalTokenCount
		s.usage.CacheRead = u.CachedContentTokenCount
	}
	for _, cand := range chunk.Candidates {
		if cand.FinishReason != "" {
			s.stopReason = googleFinishToStopReason(cand.FinishReason)
		}
		if cand.Content == nil {
			continue
		}
		for _, part := range cand.Content.Parts {
			switch {
			case part.Text != "":
				s.appendText(stream, part.Text)
			case part.FunctionCall != nil:
				s.emitToolCall(stream, part.FunctionCall)
			}
		}
	}
}

func (s *googleState) appendText(stream *ai.AssistantMessageEventStream, delta string) {
	if s.textBlock == nil {
		s.textBlock = &ai.TextContent{}
		s.partial.Content = append(s.partial.Content, s.textBlock)
		s.textIndex = len(s.partial.Content) - 1
		stream.Push(ai.AssistantMessageEvent{
			Type:         ai.EventTextStart,
			ContentIndex: s.textIndex,
			Partial:      s.partial,
		})
	}
	s.textBlock.Text += delta
	stream.Push(ai.AssistantMessageEvent{
		Type:         ai.EventTextDelta,
		ContentIndex: s.textIndex,
		Delta:        delta,
		Partial:      s.partial,
	})
}

func (s *googleState) emitToolCall(stream *ai.AssistantMessageEventStream, fc *googleFunctionCall) {
	// Close any pending text block before a tool call.
	if s.textBlock != nil {
		stream.Push(ai.AssistantMessageEvent{
			Type:         ai.EventTextEnd,
			ContentIndex: s.textIndex,
			Content:      s.textBlock.Text,
			Partial:      s.partial,
		})
		s.textBlock = nil
		s.textIndex = -1
	}
	// Gemini doesn't provide stable tool call IDs; synthesise one.
	s.toolCounter++
	tc := &ai.ToolCall{
		ID:        fmt.Sprintf("call_%d", s.toolCounter),
		Name:      fc.Name,
		Arguments: fc.Args,
	}
	if tc.Arguments == nil {
		tc.Arguments = map[string]any{}
	}
	s.partial.Content = append(s.partial.Content, tc)
	idx := len(s.partial.Content) - 1
	stream.Push(ai.AssistantMessageEvent{
		Type:         ai.EventToolCallStart,
		ContentIndex: idx,
		ToolCall:     tc,
		Partial:      s.partial,
	})
	stream.Push(ai.AssistantMessageEvent{
		Type:         ai.EventToolCallEnd,
		ContentIndex: idx,
		ToolCall:     tc,
		Partial:      s.partial,
	})
}

func (s *googleState) finish(stream *ai.AssistantMessageEventStream) {
	if s.textBlock != nil {
		stream.Push(ai.AssistantMessageEvent{
			Type:         ai.EventTextEnd,
			ContentIndex: s.textIndex,
			Content:      s.textBlock.Text,
			Partial:      s.partial,
		})
	}

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

func googleFinishToStopReason(fr string) ai.StopReason {
	switch fr {
	case "STOP":
		return ai.StopReasonStop
	case "MAX_TOKENS":
		return ai.StopReasonLength
	case "SAFETY", "RECITATION":
		return ai.StopReasonError
	}
	return ""
}

func emitGoogleError(stream *ai.AssistantMessageEventStream, model ai.Model, err error) {
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

func emitGoogleAborted(stream *ai.AssistantMessageEventStream, model ai.Model, err error) {
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
