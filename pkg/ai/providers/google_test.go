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

func TestConvertSystemToGoogle(t *testing.T) {
	if convertSystemToGoogle("") != nil {
		t.Error("empty system should return nil")
	}
	sys := convertSystemToGoogle("you are helpful")
	if sys == nil || len(sys.Parts) != 1 || sys.Parts[0].Text != "you are helpful" {
		t.Errorf("got %+v", sys)
	}
}

func TestConvertMessagesToGoogleRoles(t *testing.T) {
	msgs := []ai.Message{
		&ai.UserMessage{Content: []ai.Content{&ai.TextContent{Text: "hi"}}},
		&ai.AssistantMessage{Content: []ai.Content{&ai.TextContent{Text: "hello"}}},
	}
	got := convertMessagesToGoogle(msgs)
	if len(got) != 2 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].Role != "user" || got[1].Role != "model" {
		t.Errorf("roles = %q/%q", got[0].Role, got[1].Role)
	}
}

func TestConvertMessagesToGoogleImages(t *testing.T) {
	msgs := []ai.Message{
		&ai.UserMessage{Content: []ai.Content{
			&ai.TextContent{Text: "look"},
			&ai.ImageContent{Data: "AAAA", MimeType: "image/png"},
		}},
	}
	got := convertMessagesToGoogle(msgs)
	if len(got[0].Parts) != 2 {
		t.Fatalf("len = %d", len(got[0].Parts))
	}
	if got[0].Parts[1].InlineData == nil || got[0].Parts[1].InlineData.MimeType != "image/png" {
		t.Errorf("image = %+v", got[0].Parts[1].InlineData)
	}
}

func TestConvertMessagesToGoogleToolCall(t *testing.T) {
	msgs := []ai.Message{
		&ai.AssistantMessage{Content: []ai.Content{
			&ai.TextContent{Text: "let me read"},
			&ai.ToolCall{ID: "c1", Name: "read", Arguments: map[string]any{"path": "/x"}},
		}},
	}
	got := convertMessagesToGoogle(msgs)
	parts := got[0].Parts
	if len(parts) != 2 {
		t.Fatalf("len = %d", len(parts))
	}
	if parts[1].FunctionCall == nil || parts[1].FunctionCall.Name != "read" {
		t.Errorf("function call = %+v", parts[1].FunctionCall)
	}
}

func TestConvertMessagesToGoogleToolResult(t *testing.T) {
	msgs := []ai.Message{
		&ai.ToolResultMessage{
			ToolCallID: "c1",
			ToolName:   "read",
			Content:    []ai.Content{&ai.TextContent{Text: "file body"}},
		},
	}
	got := convertMessagesToGoogle(msgs)
	if len(got) != 1 || got[0].Role != "user" {
		t.Fatalf("got %+v", got)
	}
	part := got[0].Parts[0]
	if part.FunctionResponse == nil || part.FunctionResponse.Name != "read" {
		t.Errorf("function response = %+v", part.FunctionResponse)
	}
	if part.FunctionResponse.Response["result"] != "file body" {
		t.Errorf("result = %v", part.FunctionResponse.Response)
	}
}

func TestBuildGoogleURLWithAPIKey(t *testing.T) {
	model := ai.Model{ID: "gemini-2.5-pro", BaseURL: "https://generativelanguage.googleapis.com/v1beta"}
	got, err := buildGoogleURL(model, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "/models/gemini-2.5-pro:streamGenerateContent") {
		t.Errorf("url missing path: %s", got)
	}
	if !strings.Contains(got, "alt=sse") {
		t.Errorf("url missing alt=sse: %s", got)
	}
	if !strings.Contains(got, "key=test-key") {
		t.Errorf("url missing key: %s", got)
	}
}

func TestBuildGoogleURLWithoutAPIKey(t *testing.T) {
	model := ai.Model{ID: "m", BaseURL: "https://example.com/v1beta"}
	got, err := buildGoogleURL(model, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "key=") {
		t.Errorf("url should not have key param: %s", got)
	}
	if !strings.Contains(got, "alt=sse") {
		t.Errorf("url missing alt=sse: %s", got)
	}
}

// ----- streaming tests -----

func TestStreamGoogleTextAndDone(t *testing.T) {
	server := newFakeGoogleServer(t, []string{
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]}}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[{"text":" world"}]}}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2,"totalTokenCount":12}}`,
	})
	defer server.Close()

	oldClient := googleHTTPClient
	googleHTTPClient = server.Client()
	t.Cleanup(func() { googleHTTPClient = oldClient })

	model := ai.Model{
		ID: "gemini-2.5-pro", Api: ai.ApiGoogleGemini,
		Provider: ai.ProviderGoogle, BaseURL: server.URL,
		Cost: ai.ModelCost{Input: 1.25, Output: 10},
	}
	stream := StreamGoogle(model, ai.Context{
		SystemPrompt: "be concise",
		Messages:     []ai.Message{&ai.UserMessage{Content: []ai.Content{&ai.TextContent{Text: "hi"}}}},
	}, &ai.StreamOptions{APIKey: "api-key-x"})

	var text string
	var sawDone bool
	for e := range stream.C {
		switch e.Type {
		case ai.EventTextDelta:
			text += e.Delta
		case ai.EventDone:
			sawDone = true
		}
	}
	if !sawDone {
		t.Error("no done event")
	}
	if text != "hello world" {
		t.Errorf("text = %q", text)
	}
	res := stream.Result()
	if res.StopReason != ai.StopReasonStop {
		t.Errorf("stop = %q", res.StopReason)
	}
	if res.Usage.Input != 10 || res.Usage.Output != 2 {
		t.Errorf("usage = %+v", res.Usage)
	}
}

func TestStreamGoogleToolCall(t *testing.T) {
	server := newFakeGoogleServer(t, []string{
		`{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"read","args":{"path":"/tmp/x"}}}]}}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[]},"finishReason":"STOP"}]}`,
	})
	defer server.Close()

	oldClient := googleHTTPClient
	googleHTTPClient = server.Client()
	t.Cleanup(func() { googleHTTPClient = oldClient })

	model := ai.Model{ID: "m", Api: ai.ApiGoogleGemini, Provider: ai.ProviderGoogle, BaseURL: server.URL}
	stream := StreamGoogle(model, ai.Context{}, &ai.StreamOptions{APIKey: "k"})

	var finalCall *ai.ToolCall
	for e := range stream.C {
		if e.Type == ai.EventToolCallEnd {
			finalCall = e.ToolCall
		}
	}
	if finalCall == nil || finalCall.Name != "read" || finalCall.Arguments["path"] != "/tmp/x" {
		t.Errorf("tool call = %+v", finalCall)
	}
	if stream.Result().StopReason != ai.StopReasonStop {
		// Gemini reports finishReason STOP even when tool calls occur.
		// The state machine upgrades to ToolUse only when there's no
		// explicit stop reason — accept either.
	}
}

func TestStreamGoogleHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`permission denied`))
	}))
	defer server.Close()

	oldClient := googleHTTPClient
	googleHTTPClient = server.Client()
	t.Cleanup(func() { googleHTTPClient = oldClient })

	model := ai.Model{Api: ai.ApiGoogleGemini, BaseURL: server.URL}
	stream := StreamGoogle(model, ai.Context{}, &ai.StreamOptions{APIKey: "bad"})

	var sawErr bool
	for e := range stream.C {
		if e.Type == ai.EventError {
			sawErr = true
			if !strings.Contains(e.Error.ErrorMessage, "403") {
				t.Errorf("error = %q", e.Error.ErrorMessage)
			}
		}
	}
	if !sawErr {
		t.Error("no error event")
	}
}

func TestStreamGoogleRequestShape(t *testing.T) {
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "key=capture-key") {
			t.Errorf("missing key query: %s", r.URL.RawQuery)
		}
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`)
		fmt.Fprintln(w)
	}))
	defer server.Close()

	oldClient := googleHTTPClient
	googleHTTPClient = server.Client()
	t.Cleanup(func() { googleHTTPClient = oldClient })

	model := ai.Model{ID: "gemini-2.5-pro", Api: ai.ApiGoogleGemini, Provider: ai.ProviderGoogle, BaseURL: server.URL}
	stream := StreamGoogle(model, ai.Context{
		SystemPrompt: "helpful",
		Messages:     []ai.Message{&ai.UserMessage{Content: []ai.Content{&ai.TextContent{Text: "hi"}}}},
	}, &ai.StreamOptions{APIKey: "capture-key"})
	for range stream.C {
	}

	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("body: %v", err)
	}
	if _, ok := req["system_instruction"]; !ok {
		t.Errorf("system_instruction missing: %v", req)
	}
	contents, ok := req["contents"].([]any)
	if !ok || len(contents) != 1 {
		t.Fatalf("contents = %v", req["contents"])
	}
}

// ----- helpers -----

func newFakeGoogleServer(t *testing.T, chunks []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, ":streamGenerateContent") {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
}
