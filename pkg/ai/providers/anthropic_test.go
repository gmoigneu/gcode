package providers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gmoigneu/gcode/pkg/ai"
)

// ----- conversion tests -----

func TestConvertSystemToAnthropic(t *testing.T) {
	blocks := convertSystemToAnthropic("you are helpful", map[string]any{"type": "ephemeral"})
	if len(blocks) != 1 {
		t.Fatalf("len = %d", len(blocks))
	}
	if blocks[0].Type != "text" || blocks[0].Text != "you are helpful" {
		t.Errorf("block = %+v", blocks[0])
	}
	if blocks[0].CacheControl["type"] != "ephemeral" {
		t.Errorf("cache control missing: %+v", blocks[0].CacheControl)
	}
}

func TestConvertSystemToAnthropicEmpty(t *testing.T) {
	if blocks := convertSystemToAnthropic("", nil); blocks != nil {
		t.Errorf("empty system should be nil, got %v", blocks)
	}
}

func TestConvertMessagesToAnthropicUserAndAssistant(t *testing.T) {
	msgs := []ai.Message{
		&ai.UserMessage{Content: []ai.Content{&ai.TextContent{Text: "hi"}}},
		&ai.AssistantMessage{Content: []ai.Content{
			&ai.ThinkingContent{Thinking: "plan", ThinkingSignature: "sig"},
			&ai.TextContent{Text: "answer"},
			&ai.ToolCall{ID: "c1", Name: "read", Arguments: map[string]any{"path": "/x"}},
		}},
	}
	got := convertMessagesToAnthropic(msgs, nil)
	if len(got) != 2 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].Role != "user" || got[0].Content[0].Type != "text" {
		t.Errorf("user = %+v", got[0])
	}
	asst := got[1].Content
	if asst[0].Type != "thinking" || asst[0].Thinking != "plan" || asst[0].Signature != "sig" {
		t.Errorf("thinking block = %+v", asst[0])
	}
	if asst[1].Type != "text" {
		t.Errorf("text block = %+v", asst[1])
	}
	if asst[2].Type != "tool_use" || asst[2].Name != "read" {
		t.Errorf("tool_use block = %+v", asst[2])
	}
}

func TestConvertMessagesToAnthropicToolResultsPacked(t *testing.T) {
	msgs := []ai.Message{
		&ai.AssistantMessage{Content: []ai.Content{
			&ai.ToolCall{ID: "c1", Name: "read"},
			&ai.ToolCall{ID: "c2", Name: "bash"},
		}},
		&ai.ToolResultMessage{ToolCallID: "c1", Content: []ai.Content{&ai.TextContent{Text: "ok1"}}},
		&ai.ToolResultMessage{ToolCallID: "c2", Content: []ai.Content{&ai.TextContent{Text: "ok2"}}},
	}
	got := convertMessagesToAnthropic(msgs, nil)
	if len(got) != 2 {
		t.Fatalf("len = %d", len(got))
	}
	if got[1].Role != "user" {
		t.Errorf("packed tool results should be user message, got %q", got[1].Role)
	}
	if len(got[1].Content) != 2 {
		t.Errorf("packed content len = %d", len(got[1].Content))
	}
	for _, c := range got[1].Content {
		if c.Type != "tool_result" {
			t.Errorf("block type = %q", c.Type)
		}
	}
}

func TestConvertMessagesToAnthropicImage(t *testing.T) {
	msgs := []ai.Message{
		&ai.UserMessage{Content: []ai.Content{
			&ai.TextContent{Text: "look"},
			&ai.ImageContent{Data: "AAAA", MimeType: "image/png"},
		}},
	}
	got := convertMessagesToAnthropic(msgs, nil)
	if len(got[0].Content) != 2 {
		t.Fatalf("len = %d", len(got[0].Content))
	}
	img := got[0].Content[1]
	if img.Type != "image" || img.Source == nil || img.Source.MediaType != "image/png" {
		t.Errorf("image block = %+v", img)
	}
}

func TestConvertMessagesToAnthropicCacheControlOnLastUser(t *testing.T) {
	msgs := []ai.Message{
		&ai.UserMessage{Content: []ai.Content{&ai.TextContent{Text: "first"}}},
		&ai.AssistantMessage{Content: []ai.Content{&ai.TextContent{Text: "reply"}}},
		&ai.UserMessage{Content: []ai.Content{
			&ai.TextContent{Text: "second"},
			&ai.TextContent{Text: "third"},
		}},
	}
	cc := map[string]any{"type": "ephemeral"}
	got := convertMessagesToAnthropic(msgs, cc)
	last := got[2]
	final := last.Content[len(last.Content)-1]
	if final.CacheControl["type"] != "ephemeral" {
		t.Errorf("cache control not placed on last user content: %+v", final)
	}
	if got[0].Content[0].CacheControl != nil {
		t.Errorf("first user message should not be tagged")
	}
}

// ----- streaming tests -----

func TestStreamAnthropicTextAndDone(t *testing.T) {
	server := newFakeAnthropicServer(t, []anthropicTestEvent{
		{Event: "message_start", Data: `{"type":"message_start","message":{"id":"m1","role":"assistant","model":"claude","usage":{"input_tokens":10,"output_tokens":0}}}`},
		{Event: "content_block_start", Data: `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
		{Event: "content_block_delta", Data: `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`},
		{Event: "content_block_delta", Data: `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}`},
		{Event: "content_block_stop", Data: `{"type":"content_block_stop","index":0}`},
		{Event: "message_delta", Data: `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`},
		{Event: "message_stop", Data: `{"type":"message_stop"}`},
	})
	defer server.Close()

	oldClient := anthropicHTTPClient
	anthropicHTTPClient = server.Client()
	t.Cleanup(func() { anthropicHTTPClient = oldClient })

	model := ai.Model{
		ID: "claude-opus-4-6", Api: ai.ApiAnthropicMessages,
		Provider: ai.ProviderAnthropic, BaseURL: server.URL,
		Cost: ai.ModelCost{Input: 15, Output: 75},
	}
	stream := StreamAnthropic(model, ai.Context{
		SystemPrompt: "you are helpful",
		Messages:     []ai.Message{&ai.UserMessage{Content: []ai.Content{&ai.TextContent{Text: "hi"}}}},
	}, &ai.StreamOptions{APIKey: "test"})

	var text string
	var sawStart, sawEnd, sawDone bool
	for e := range stream.C {
		switch e.Type {
		case ai.EventTextStart:
			sawStart = true
		case ai.EventTextDelta:
			text += e.Delta
		case ai.EventTextEnd:
			sawEnd = true
		case ai.EventDone:
			sawDone = true
		}
	}
	if !sawStart || !sawEnd || !sawDone {
		t.Errorf("missing events start=%v end=%v done=%v", sawStart, sawEnd, sawDone)
	}
	if text != "hello world" {
		t.Errorf("text = %q", text)
	}
	res := stream.Result()
	if res.StopReason != ai.StopReasonStop {
		t.Errorf("stop = %q", res.StopReason)
	}
	if res.Usage.Input != 10 || res.Usage.Output != 5 {
		t.Errorf("usage = %+v", res.Usage)
	}
	if res.Usage.Cost.Total == 0 {
		t.Error("cost not populated")
	}
}

func TestStreamAnthropicToolCall(t *testing.T) {
	server := newFakeAnthropicServer(t, []anthropicTestEvent{
		{Event: "message_start", Data: `{"type":"message_start","message":{"id":"m1","role":"assistant","model":"claude"}}`},
		{Event: "content_block_start", Data: `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"c1","name":"read","input":{}}}`},
		{Event: "content_block_delta", Data: `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}`},
		{Event: "content_block_delta", Data: `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"/tmp/x\"}"}}`},
		{Event: "content_block_stop", Data: `{"type":"content_block_stop","index":0}`},
		{Event: "message_delta", Data: `{"type":"message_delta","delta":{"stop_reason":"tool_use"}}`},
		{Event: "message_stop", Data: `{"type":"message_stop"}`},
	})
	defer server.Close()

	oldClient := anthropicHTTPClient
	anthropicHTTPClient = server.Client()
	t.Cleanup(func() { anthropicHTTPClient = oldClient })

	model := ai.Model{Api: ai.ApiAnthropicMessages, Provider: ai.ProviderAnthropic, BaseURL: server.URL}
	stream := StreamAnthropic(model, ai.Context{}, &ai.StreamOptions{APIKey: "k"})

	var start, end int
	var finalCall *ai.ToolCall
	for e := range stream.C {
		switch e.Type {
		case ai.EventToolCallStart:
			start++
		case ai.EventToolCallEnd:
			end++
			finalCall = e.ToolCall
		}
	}
	if start != 1 || end != 1 {
		t.Errorf("start/end = %d/%d", start, end)
	}
	if finalCall == nil || finalCall.Name != "read" || finalCall.Arguments["path"] != "/tmp/x" {
		t.Errorf("tool call = %+v", finalCall)
	}
	if stream.Result().StopReason != ai.StopReasonToolUse {
		t.Errorf("stop = %q", stream.Result().StopReason)
	}
}

func TestStreamAnthropicThinking(t *testing.T) {
	server := newFakeAnthropicServer(t, []anthropicTestEvent{
		{Event: "message_start", Data: `{"type":"message_start","message":{"id":"m1","role":"assistant","model":"claude"}}`},
		{Event: "content_block_start", Data: `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","text":""}}`},
		{Event: "content_block_delta", Data: `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"pondering"}}`},
		{Event: "content_block_stop", Data: `{"type":"content_block_stop","index":0}`},
		{Event: "content_block_start", Data: `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`},
		{Event: "content_block_delta", Data: `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"done"}}`},
		{Event: "content_block_stop", Data: `{"type":"content_block_stop","index":1}`},
		{Event: "message_delta", Data: `{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`},
		{Event: "message_stop", Data: `{"type":"message_stop"}`},
	})
	defer server.Close()

	oldClient := anthropicHTTPClient
	anthropicHTTPClient = server.Client()
	t.Cleanup(func() { anthropicHTTPClient = oldClient })

	model := ai.Model{Api: ai.ApiAnthropicMessages, Provider: ai.ProviderAnthropic, BaseURL: server.URL}
	stream := StreamAnthropic(model, ai.Context{}, &ai.StreamOptions{APIKey: "k"})

	var think, text string
	for e := range stream.C {
		switch e.Type {
		case ai.EventThinkingDelta:
			think += e.Delta
		case ai.EventTextDelta:
			text += e.Delta
		}
	}
	if think != "pondering" {
		t.Errorf("thinking = %q", think)
	}
	if text != "done" {
		t.Errorf("text = %q", text)
	}
}

func TestStreamAnthropicHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid"}}`))
	}))
	defer server.Close()

	oldClient := anthropicHTTPClient
	anthropicHTTPClient = server.Client()
	t.Cleanup(func() { anthropicHTTPClient = oldClient })

	model := ai.Model{Api: ai.ApiAnthropicMessages, BaseURL: server.URL}
	stream := StreamAnthropic(model, ai.Context{}, &ai.StreamOptions{APIKey: "k"})

	var gotErr bool
	for e := range stream.C {
		if e.Type == ai.EventError {
			gotErr = true
			if !strings.Contains(e.Error.ErrorMessage, "401") {
				t.Errorf("error = %q", e.Error.ErrorMessage)
			}
		}
	}
	if !gotErr {
		t.Error("no error event emitted")
	}
}

func TestStreamAnthropicSendsHeaders(t *testing.T) {
	var capturedVersion, capturedKey string
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedVersion = r.Header.Get("anthropic-version")
		capturedKey = r.Header.Get("x-api-key")
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, "event: message_delta")
		fmt.Fprintln(w, `data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "event: message_stop")
		fmt.Fprintln(w, `data: {"type":"message_stop"}`)
		fmt.Fprintln(w)
	}))
	defer server.Close()

	oldClient := anthropicHTTPClient
	anthropicHTTPClient = server.Client()
	t.Cleanup(func() { anthropicHTTPClient = oldClient })

	model := ai.Model{Api: ai.ApiAnthropicMessages, BaseURL: server.URL, ID: "claude-opus-4-6"}
	stream := StreamAnthropic(model, ai.Context{}, &ai.StreamOptions{APIKey: "secret"})
	for range stream.C {
	}

	if capturedVersion != "2023-06-01" {
		t.Errorf("anthropic-version = %q", capturedVersion)
	}
	if capturedKey != "secret" {
		t.Errorf("x-api-key = %q", capturedKey)
	}
	// Request body should contain system field if present; since no system
	// prompt was passed it should have messages but no system.
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("body: %v", err)
	}
	if _, ok := req["system"]; ok {
		t.Errorf("system should be omitted when empty: %v", req["system"])
	}
}

// ----- helpers -----

type anthropicTestEvent struct {
	Event string
	Data  string
}

func newFakeAnthropicServer(t *testing.T, events []anthropicTestEvent) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for _, e := range events {
			fmt.Fprintf(w, "event: %s\n", e.Event)
			fmt.Fprintf(w, "data: %s\n\n", e.Data)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
}
