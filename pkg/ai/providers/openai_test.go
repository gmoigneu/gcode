package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gmoigneu/gcode/pkg/ai"
)

// ----- message conversion -----

func TestConvertUserMessageOpenAISimpleText(t *testing.T) {
	msg := &ai.UserMessage{Content: []ai.Content{&ai.TextContent{Text: "hi"}}}
	out := convertUserMessageOpenAI(msg)
	if out.Role != "user" {
		t.Errorf("role = %q", out.Role)
	}
	if out.Content != "hi" {
		t.Errorf("content = %v", out.Content)
	}
}

func TestConvertUserMessageOpenAIWithImage(t *testing.T) {
	msg := &ai.UserMessage{Content: []ai.Content{
		&ai.TextContent{Text: "look"},
		&ai.ImageContent{Data: "AAAA", MimeType: "image/png"},
	}}
	out := convertUserMessageOpenAI(msg)
	parts, ok := out.Content.([]openAIPart)
	if !ok {
		t.Fatalf("content type = %T", out.Content)
	}
	if len(parts) != 2 || parts[0].Type != "text" || parts[1].Type != "image_url" {
		t.Errorf("parts = %+v", parts)
	}
	if !strings.HasPrefix(parts[1].ImageURL.URL, "data:image/png;base64,") {
		t.Errorf("image url = %q", parts[1].ImageURL.URL)
	}
}

func TestConvertAssistantMessageOpenAIToolCall(t *testing.T) {
	msg := &ai.AssistantMessage{Content: []ai.Content{
		&ai.TextContent{Text: "let me read"},
		&ai.ToolCall{ID: "c1", Name: "read", Arguments: map[string]any{"path": "/tmp/x"}},
	}}
	out := convertAssistantMessageOpenAI(msg, ai.OpenAICompat{})
	if out.Role != "assistant" || out.Content != "let me read" {
		t.Errorf("got %+v", out)
	}
	if len(out.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d", len(out.ToolCalls))
	}
	tc := out.ToolCalls[0]
	if tc.ID != "c1" || tc.Function.Name != "read" {
		t.Errorf("tool call = %+v", tc)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		t.Errorf("arguments not JSON: %v", err)
	}
	if args["path"] != "/tmp/x" {
		t.Errorf("args = %v", args)
	}
}

func TestConvertAssistantMessageOpenAIThinkingAsText(t *testing.T) {
	msg := &ai.AssistantMessage{Content: []ai.Content{
		&ai.ThinkingContent{Thinking: "pondering"},
		&ai.TextContent{Text: "answer"},
	}}
	out := convertAssistantMessageOpenAI(msg, ai.OpenAICompat{RequiresThinkingAsText: true})
	content := out.Content.(string)
	if !strings.Contains(content, "<thinking>pondering</thinking>") {
		t.Errorf("content = %q", content)
	}
	if !strings.Contains(content, "answer") {
		t.Errorf("content missing answer: %q", content)
	}
}

func TestConvertToolResultMessageOpenAI(t *testing.T) {
	msg := &ai.ToolResultMessage{
		ToolCallID: "c1",
		ToolName:   "read",
		Content:    []ai.Content{&ai.TextContent{Text: "file data"}},
	}
	got := convertToolResultMessageOpenAI(msg, ai.OpenAICompat{})
	if len(got) != 1 {
		t.Fatalf("got %d", len(got))
	}
	if got[0].Role != "tool" || got[0].ToolCallID != "c1" {
		t.Errorf("got %+v", got[0])
	}
	if got[0].Content != "file data" {
		t.Errorf("content = %v", got[0].Content)
	}
	if got[0].Name != "" {
		t.Errorf("name should not be set for default compat")
	}
}

func TestConvertToolResultMessageOpenAIRequiresName(t *testing.T) {
	msg := &ai.ToolResultMessage{ToolCallID: "c1", ToolName: "read", Content: []ai.Content{&ai.TextContent{Text: "ok"}}}
	got := convertToolResultMessageOpenAI(msg, ai.OpenAICompat{RequiresToolResultName: true})
	if got[0].Name != "read" {
		t.Errorf("name = %q", got[0].Name)
	}
}

func TestConvertToolResultMessageOpenAIWithImage(t *testing.T) {
	msg := &ai.ToolResultMessage{
		ToolCallID: "c1",
		ToolName:   "read",
		Content: []ai.Content{
			&ai.TextContent{Text: "here is the file"},
			&ai.ImageContent{Data: "AAAA", MimeType: "image/png"},
		},
	}
	got := convertToolResultMessageOpenAI(msg, ai.OpenAICompat{})
	if len(got) != 2 {
		t.Fatalf("expected tool + user messages, got %d", len(got))
	}
	if got[1].Role != "user" {
		t.Errorf("image follow-up role = %q", got[1].Role)
	}
}

func TestConvertMessagesOpenAISystemPromptDeveloperRole(t *testing.T) {
	msgs := convertMessagesToOpenAI("you are helpful", []ai.Message{
		&ai.UserMessage{Content: []ai.Content{&ai.TextContent{Text: "hi"}}},
	}, ai.OpenAICompat{SupportsDeveloperRole: true})
	if msgs[0].Role != "developer" {
		t.Errorf("system role = %q, want developer", msgs[0].Role)
	}
}

func TestConvertMessagesOpenAISystemPromptDefaultRole(t *testing.T) {
	msgs := convertMessagesToOpenAI("you are helpful", []ai.Message{
		&ai.UserMessage{Content: []ai.Content{&ai.TextContent{Text: "hi"}}},
	}, ai.OpenAICompat{})
	if msgs[0].Role != "system" {
		t.Errorf("system role = %q", msgs[0].Role)
	}
}

// ----- request marshal -----

func TestOpenAIRequestMarshalMaxTokensField(t *testing.T) {
	max := 128
	req := openAIRequest{
		Model:          "m",
		Messages:       []openAIMessage{{Role: "user", Content: "hi"}},
		Stream:         true,
		MaxTokens:      &max,
		MaxTokensField: "max_completion_tokens",
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"max_completion_tokens":128`) {
		t.Errorf("body missing max_completion_tokens: %s", string(b))
	}
	if strings.Contains(string(b), `"max_tokens":128`) {
		t.Errorf("body should not have max_tokens when override set: %s", string(b))
	}
}

func TestOpenAIRequestMarshalDefaultMaxTokens(t *testing.T) {
	max := 64
	req := openAIRequest{
		Model:     "m",
		MaxTokens: &max,
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"max_tokens":64`) {
		t.Errorf("body missing max_tokens: %s", string(b))
	}
}

// ----- end-to-end stream test with httptest.Server -----

func TestStreamOpenAITextAndDone(t *testing.T) {
	server := newFakeOpenAIServer(t, []string{
		`{"id":"1","choices":[{"index":0,"delta":{"content":"hello"}}]}`,
		`{"id":"1","choices":[{"index":0,"delta":{"content":" world"}}]}`,
		`{"id":"1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`,
	})
	defer server.Close()

	oldClient := openAIHTTPClient
	openAIHTTPClient = server.Client()
	t.Cleanup(func() { openAIHTTPClient = oldClient })

	model := ai.Model{
		ID: "gpt-4.1", Api: ai.ApiOpenAICompletions,
		Provider: ai.ProviderOpenAI, BaseURL: server.URL,
		Cost: ai.ModelCost{Input: 2.5, Output: 10},
	}
	stream := StreamOpenAI(model, ai.Context{
		Messages: []ai.Message{&ai.UserMessage{Content: []ai.Content{&ai.TextContent{Text: "hi"}}}},
	}, &ai.StreamOptions{APIKey: "test-key"})

	var text string
	var sawTextStart, sawTextEnd, sawDone bool
	for e := range stream.C {
		switch e.Type {
		case ai.EventTextStart:
			sawTextStart = true
		case ai.EventTextDelta:
			text += e.Delta
		case ai.EventTextEnd:
			sawTextEnd = true
		case ai.EventDone:
			sawDone = true
		}
	}
	if !sawTextStart || !sawTextEnd || !sawDone {
		t.Errorf("missing events start=%v end=%v done=%v", sawTextStart, sawTextEnd, sawDone)
	}
	if text != "hello world" {
		t.Errorf("text = %q", text)
	}
	res := stream.Result()
	if res.StopReason != ai.StopReasonStop {
		t.Errorf("stopReason = %q", res.StopReason)
	}
	if res.Usage.Input != 10 || res.Usage.Output != 2 {
		t.Errorf("usage = %+v", res.Usage)
	}
	if res.Usage.Cost.Total == 0 {
		t.Error("cost not populated")
	}
}

func TestStreamOpenAIToolCall(t *testing.T) {
	server := newFakeOpenAIServer(t, []string{
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"read"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"/tmp/x\"}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	})
	defer server.Close()

	oldClient := openAIHTTPClient
	openAIHTTPClient = server.Client()
	t.Cleanup(func() { openAIHTTPClient = oldClient })

	model := ai.Model{ID: "gpt-4.1", Api: ai.ApiOpenAICompletions, Provider: ai.ProviderOpenAI, BaseURL: server.URL}
	stream := StreamOpenAI(model, ai.Context{}, &ai.StreamOptions{APIKey: "k"})

	var started, ended int
	var finalCall *ai.ToolCall
	for e := range stream.C {
		switch e.Type {
		case ai.EventToolCallStart:
			started++
		case ai.EventToolCallEnd:
			ended++
			finalCall = e.ToolCall
		}
	}
	if started != 1 || ended != 1 {
		t.Errorf("start/end = %d/%d", started, ended)
	}
	if finalCall == nil || finalCall.Name != "read" {
		t.Fatalf("finalCall = %+v", finalCall)
	}
	if finalCall.Arguments["path"] != "/tmp/x" {
		t.Errorf("args = %v", finalCall.Arguments)
	}
	if stream.Result().StopReason != ai.StopReasonToolUse {
		t.Errorf("stopReason = %q", stream.Result().StopReason)
	}
}

func TestStreamOpenAIReasoning(t *testing.T) {
	server := newFakeOpenAIServer(t, []string{
		`{"choices":[{"index":0,"delta":{"reasoning_content":"let me think"}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"done"}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	})
	defer server.Close()

	oldClient := openAIHTTPClient
	openAIHTTPClient = server.Client()
	t.Cleanup(func() { openAIHTTPClient = oldClient })

	model := ai.Model{Api: ai.ApiOpenAICompletions, Provider: ai.ProviderOpenAI, BaseURL: server.URL}
	stream := StreamOpenAI(model, ai.Context{}, &ai.StreamOptions{})

	var thinking, text string
	for e := range stream.C {
		switch e.Type {
		case ai.EventThinkingDelta:
			thinking += e.Delta
		case ai.EventTextDelta:
			text += e.Delta
		}
	}
	if thinking != "let me think" {
		t.Errorf("thinking = %q", thinking)
	}
	if text != "done" {
		t.Errorf("text = %q", text)
	}
}

func TestStreamOpenAIHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	defer server.Close()

	oldClient := openAIHTTPClient
	openAIHTTPClient = server.Client()
	t.Cleanup(func() { openAIHTTPClient = oldClient })

	model := ai.Model{Api: ai.ApiOpenAICompletions, BaseURL: server.URL}
	stream := StreamOpenAI(model, ai.Context{}, &ai.StreamOptions{APIKey: "bad"})

	var sawErr bool
	for e := range stream.C {
		if e.Type == ai.EventError {
			sawErr = true
			if !strings.Contains(e.Error.ErrorMessage, "401") {
				t.Errorf("error = %q", e.Error.ErrorMessage)
			}
		}
	}
	if !sawErr {
		t.Error("no error event")
	}
	if stream.Result().StopReason != ai.StopReasonError {
		t.Errorf("stop = %q", stream.Result().StopReason)
	}
}

func TestStreamOpenAIContextCancellation(t *testing.T) {
	// Server that hangs until request is cancelled.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		// push one chunk then hang
		_, _ = fmt.Fprintf(w, "data: %s\n\n", `{"choices":[{"index":0,"delta":{"content":"hi"}}]}`)
		if flusher != nil {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	defer server.Close()

	oldClient := openAIHTTPClient
	openAIHTTPClient = server.Client()
	t.Cleanup(func() { openAIHTTPClient = oldClient })

	ctx, cancel := context.WithCancel(context.Background())
	model := ai.Model{Api: ai.ApiOpenAICompletions, BaseURL: server.URL}
	stream := StreamOpenAI(model, ai.Context{}, &ai.StreamOptions{APIKey: "k", Signal: ctx})

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	var sawErr bool
	timeout := time.After(5 * time.Second)
drain:
	for {
		select {
		case e, ok := <-stream.C:
			if !ok {
				break drain
			}
			if e.Type == ai.EventError {
				sawErr = true
			}
		case <-timeout:
			t.Fatal("stream did not terminate within timeout")
		}
	}
	_ = sawErr // cancellation may emit an error; what matters is the stream closed
}

// ----- helpers -----

// newFakeOpenAIServer returns an httptest.Server that writes the provided
// JSON chunks as SSE data lines, followed by [DONE].
func newFakeOpenAIServer(t *testing.T, chunks []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %q", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"stream":true`) {
			t.Errorf("stream not set in body")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for _, c := range chunks {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", c)
			if flusher != nil {
				flusher.Flush()
			}
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
	}))
}
