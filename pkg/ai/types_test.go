package ai

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// ---------- Content type identity ----------

func TestContentTypeIdentity(t *testing.T) {
	cases := []struct {
		name string
		c    Content
		want string
	}{
		{"text", &TextContent{Text: "hi"}, "text"},
		{"thinking", &ThinkingContent{Thinking: "hmm"}, "thinking"},
		{"image", &ImageContent{Data: "AAAA", MimeType: "image/png"}, "image"},
		{"toolCall", &ToolCall{ID: "call_1", Name: "read"}, "toolCall"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.c.ContentType(); got != tc.want {
				t.Fatalf("ContentType() = %q, want %q", got, tc.want)
			}
		})
	}
}

// ---------- Content marshal: type field is populated ----------

func TestTextContentMarshal(t *testing.T) {
	c := &TextContent{Text: "hello"}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got["type"] != "text" {
		t.Errorf("type = %v, want text", got["type"])
	}
	if got["text"] != "hello" {
		t.Errorf("text = %v, want hello", got["text"])
	}
}

func TestToolCallMarshal(t *testing.T) {
	c := &ToolCall{
		ID:        "call_1",
		Name:      "read",
		Arguments: map[string]any{"path": "/tmp/x"},
	}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got["type"] != "toolCall" {
		t.Errorf("type = %v, want toolCall", got["type"])
	}
	if got["id"] != "call_1" {
		t.Errorf("id = %v", got["id"])
	}
	args, ok := got["arguments"].(map[string]any)
	if !ok {
		t.Fatalf("arguments not an object: %T", got["arguments"])
	}
	if args["path"] != "/tmp/x" {
		t.Errorf("arguments.path = %v", args["path"])
	}
}

// ---------- Content unmarshal: discriminated by type field ----------

func TestUnmarshalContent_Text(t *testing.T) {
	raw := []byte(`{"type":"text","text":"hello"}`)
	c, err := UnmarshalContent(raw)
	if err != nil {
		t.Fatal(err)
	}
	tc, ok := c.(*TextContent)
	if !ok {
		t.Fatalf("got %T, want *TextContent", c)
	}
	if tc.Text != "hello" {
		t.Errorf("Text = %q", tc.Text)
	}
}

func TestUnmarshalContent_Thinking(t *testing.T) {
	raw := []byte(`{"type":"thinking","thinking":"pondering","thinkingSignature":"sig"}`)
	c, err := UnmarshalContent(raw)
	if err != nil {
		t.Fatal(err)
	}
	tc, ok := c.(*ThinkingContent)
	if !ok {
		t.Fatalf("got %T, want *ThinkingContent", c)
	}
	if tc.Thinking != "pondering" || tc.ThinkingSignature != "sig" {
		t.Errorf("got %+v", tc)
	}
}

func TestUnmarshalContent_Image(t *testing.T) {
	raw := []byte(`{"type":"image","data":"AAAA","mimeType":"image/png"}`)
	c, err := UnmarshalContent(raw)
	if err != nil {
		t.Fatal(err)
	}
	ic, ok := c.(*ImageContent)
	if !ok {
		t.Fatalf("got %T, want *ImageContent", c)
	}
	if ic.Data != "AAAA" || ic.MimeType != "image/png" {
		t.Errorf("got %+v", ic)
	}
}

func TestUnmarshalContent_ToolCall(t *testing.T) {
	raw := []byte(`{"type":"toolCall","id":"c1","name":"read","arguments":{"path":"/a"}}`)
	c, err := UnmarshalContent(raw)
	if err != nil {
		t.Fatal(err)
	}
	tc, ok := c.(*ToolCall)
	if !ok {
		t.Fatalf("got %T, want *ToolCall", c)
	}
	if tc.ID != "c1" || tc.Name != "read" {
		t.Errorf("got %+v", tc)
	}
	if tc.Arguments["path"] != "/a" {
		t.Errorf("arguments = %v", tc.Arguments)
	}
}

func TestUnmarshalContent_UnknownType(t *testing.T) {
	raw := []byte(`{"type":"mystery","text":"?"}`)
	if _, err := UnmarshalContent(raw); err == nil {
		t.Fatal("expected error for unknown content type")
	}
}

func TestUnmarshalContent_MissingType(t *testing.T) {
	raw := []byte(`{"text":"hi"}`)
	if _, err := UnmarshalContent(raw); err == nil {
		t.Fatal("expected error for missing type")
	}
}

// ---------- UserMessage round-trip ----------

func TestUserMessageRoundTrip(t *testing.T) {
	orig := &UserMessage{
		Content: []Content{
			&TextContent{Text: "hello"},
			&ImageContent{Data: "AAAA", MimeType: "image/png"},
		},
		Timestamp: 1700000000000,
	}
	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	// Role must be injected during marshal
	var wire map[string]any
	if err := json.Unmarshal(b, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["role"] != "user" {
		t.Errorf("role = %v, want user", wire["role"])
	}

	var got UserMessage
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Content) != 2 {
		t.Fatalf("content len = %d", len(got.Content))
	}
	if tc, ok := got.Content[0].(*TextContent); !ok || tc.Text != "hello" {
		t.Errorf("content[0] = %+v", got.Content[0])
	}
	if ic, ok := got.Content[1].(*ImageContent); !ok || ic.Data != "AAAA" {
		t.Errorf("content[1] = %+v", got.Content[1])
	}
	if got.Timestamp != 1700000000000 {
		t.Errorf("timestamp = %d", got.Timestamp)
	}
}

// ---------- AssistantMessage round-trip ----------

func TestAssistantMessageRoundTrip(t *testing.T) {
	orig := &AssistantMessage{
		Content: []Content{
			&ThinkingContent{Thinking: "plan"},
			&TextContent{Text: "done"},
			&ToolCall{ID: "c1", Name: "read", Arguments: map[string]any{"path": "/x"}},
		},
		Api:        ApiAnthropicMessages,
		Provider:   ProviderAnthropic,
		Model:      "claude-opus-4-6",
		ResponseID: "resp_1",
		Usage: Usage{
			Input: 100, Output: 50, CacheRead: 10, CacheWrite: 5, TotalTokens: 165,
			Cost: Cost{Input: 0.003, Output: 0.0075, Total: 0.0105},
		},
		StopReason: StopReasonStop,
		Timestamp:  1700000000000,
	}
	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}

	var wire map[string]any
	if err := json.Unmarshal(b, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["role"] != "assistant" {
		t.Errorf("role = %v", wire["role"])
	}

	var got AssistantMessage
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Api != ApiAnthropicMessages || got.Provider != ProviderAnthropic {
		t.Errorf("api/provider = %q/%q", got.Api, got.Provider)
	}
	if got.Model != "claude-opus-4-6" || got.ResponseID != "resp_1" {
		t.Errorf("model/responseId = %q/%q", got.Model, got.ResponseID)
	}
	if got.Usage.Input != 100 || got.Usage.Output != 50 || got.Usage.TotalTokens != 165 {
		t.Errorf("usage = %+v", got.Usage)
	}
	if got.StopReason != StopReasonStop {
		t.Errorf("stopReason = %q", got.StopReason)
	}
	if len(got.Content) != 3 {
		t.Fatalf("content len = %d", len(got.Content))
	}
	if _, ok := got.Content[0].(*ThinkingContent); !ok {
		t.Errorf("content[0] = %T", got.Content[0])
	}
	if _, ok := got.Content[1].(*TextContent); !ok {
		t.Errorf("content[1] = %T", got.Content[1])
	}
	tc, ok := got.Content[2].(*ToolCall)
	if !ok {
		t.Fatalf("content[2] = %T", got.Content[2])
	}
	if tc.ID != "c1" || tc.Name != "read" || tc.Arguments["path"] != "/x" {
		t.Errorf("toolCall = %+v", tc)
	}
}

// ---------- ToolResultMessage round-trip ----------

func TestToolResultMessageRoundTrip(t *testing.T) {
	orig := &ToolResultMessage{
		ToolCallID: "c1",
		ToolName:   "read",
		Content:    []Content{&TextContent{Text: "file contents"}},
		IsError:    false,
		Timestamp:  1700000000000,
	}
	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}

	var wire map[string]any
	if err := json.Unmarshal(b, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["role"] != "toolResult" {
		t.Errorf("role = %v", wire["role"])
	}
	if wire["toolCallId"] != "c1" {
		t.Errorf("toolCallId = %v", wire["toolCallId"])
	}

	var got ToolResultMessage
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.ToolCallID != "c1" || got.ToolName != "read" || got.IsError {
		t.Errorf("got %+v", got)
	}
	if len(got.Content) != 1 {
		t.Fatalf("content len = %d", len(got.Content))
	}
	if tc, ok := got.Content[0].(*TextContent); !ok || tc.Text != "file contents" {
		t.Errorf("content[0] = %+v", got.Content[0])
	}
}

// ---------- Message interface ----------

func TestMessageInterface(t *testing.T) {
	var m Message = &UserMessage{Timestamp: 1}
	if m.MessageRole() != "user" || m.MessageTimestamp() != 1 {
		t.Errorf("user: role=%q ts=%d", m.MessageRole(), m.MessageTimestamp())
	}
	m = &AssistantMessage{Timestamp: 2}
	if m.MessageRole() != "assistant" || m.MessageTimestamp() != 2 {
		t.Errorf("assistant: role=%q ts=%d", m.MessageRole(), m.MessageTimestamp())
	}
	m = &ToolResultMessage{Timestamp: 3}
	if m.MessageRole() != "toolResult" || m.MessageTimestamp() != 3 {
		t.Errorf("toolResult: role=%q ts=%d", m.MessageRole(), m.MessageTimestamp())
	}
}

// ---------- UnmarshalMessage: discriminated by role ----------

func TestUnmarshalMessage_User(t *testing.T) {
	raw := []byte(`{"role":"user","content":[{"type":"text","text":"hi"}],"timestamp":1}`)
	m, err := UnmarshalMessage(raw)
	if err != nil {
		t.Fatal(err)
	}
	u, ok := m.(*UserMessage)
	if !ok {
		t.Fatalf("got %T, want *UserMessage", m)
	}
	if len(u.Content) != 1 {
		t.Fatalf("content len = %d", len(u.Content))
	}
}

func TestUnmarshalMessage_Assistant(t *testing.T) {
	raw := []byte(`{"role":"assistant","content":[{"type":"text","text":"ok"}],"api":"anthropic-messages","provider":"anthropic","model":"m","usage":{"input":1,"output":1,"cacheRead":0,"cacheWrite":0,"totalTokens":2,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"stopReason":"stop","timestamp":1}`)
	m, err := UnmarshalMessage(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.(*AssistantMessage); !ok {
		t.Fatalf("got %T, want *AssistantMessage", m)
	}
}

func TestUnmarshalMessage_ToolResult(t *testing.T) {
	raw := []byte(`{"role":"toolResult","toolCallId":"c1","toolName":"read","content":[{"type":"text","text":"data"}],"isError":false,"timestamp":1}`)
	m, err := UnmarshalMessage(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.(*ToolResultMessage); !ok {
		t.Fatalf("got %T, want *ToolResultMessage", m)
	}
}

func TestUnmarshalMessage_UnknownRole(t *testing.T) {
	raw := []byte(`{"role":"system","content":[]}`)
	if _, err := UnmarshalMessage(raw); err == nil {
		t.Fatal("expected error for unknown role")
	}
}

func TestUnmarshalMessage_MissingRole(t *testing.T) {
	raw := []byte(`{"content":[]}`)
	if _, err := UnmarshalMessage(raw); err == nil {
		t.Fatal("expected error for missing role")
	}
}

// ---------- Context round-trip with mixed messages ----------

func TestContextRoundTrip(t *testing.T) {
	orig := &Context{
		SystemPrompt: "you are helpful",
		Messages: []Message{
			&UserMessage{Content: []Content{&TextContent{Text: "hi"}}, Timestamp: 1},
			&AssistantMessage{
				Content:    []Content{&TextContent{Text: "hello"}},
				Api:        ApiAnthropicMessages,
				Provider:   ProviderAnthropic,
				Model:      "m",
				Usage:      Usage{Input: 1, Output: 1, TotalTokens: 2},
				StopReason: StopReasonStop,
				Timestamp:  2,
			},
			&ToolResultMessage{
				ToolCallID: "c1",
				ToolName:   "read",
				Content:    []Content{&TextContent{Text: "ok"}},
				Timestamp:  3,
			},
		},
		Tools: []Tool{
			{Name: "read", Description: "read a file", Parameters: json.RawMessage(`{"type":"object"}`)},
		},
	}

	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}

	var got Context
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.SystemPrompt != "you are helpful" {
		t.Errorf("systemPrompt = %q", got.SystemPrompt)
	}
	if len(got.Messages) != 3 {
		t.Fatalf("messages len = %d", len(got.Messages))
	}
	if _, ok := got.Messages[0].(*UserMessage); !ok {
		t.Errorf("messages[0] = %T", got.Messages[0])
	}
	if _, ok := got.Messages[1].(*AssistantMessage); !ok {
		t.Errorf("messages[1] = %T", got.Messages[1])
	}
	if _, ok := got.Messages[2].(*ToolResultMessage); !ok {
		t.Errorf("messages[2] = %T", got.Messages[2])
	}
	if len(got.Tools) != 1 || got.Tools[0].Name != "read" {
		t.Errorf("tools = %+v", got.Tools)
	}
	if !strings.Contains(string(got.Tools[0].Parameters), "object") {
		t.Errorf("parameters = %s", got.Tools[0].Parameters)
	}
}

// ---------- Constants ----------

func TestApiConstants(t *testing.T) {
	cases := map[Api]string{
		ApiOpenAICompletions: "openai-completions",
		ApiAnthropicMessages: "anthropic-messages",
		ApiGoogleGemini:      "google-gemini",
	}
	for got, want := range cases {
		if string(got) != want {
			t.Errorf("%s = %q, want %q", want, got, want)
		}
	}
}

func TestProviderConstants(t *testing.T) {
	cases := map[Provider]string{
		ProviderOpenAI:     "openai",
		ProviderAnthropic:  "anthropic",
		ProviderXAI:        "xai",
		ProviderGroq:       "groq",
		ProviderCerebras:   "cerebras",
		ProviderOpenRouter: "openrouter",
		ProviderGoogle:     "google",
	}
	for got, want := range cases {
		if string(got) != want {
			t.Errorf("%s = %q, want %q", want, got, want)
		}
	}
}

func TestStopReasonConstants(t *testing.T) {
	cases := map[StopReason]string{
		StopReasonStop:    "stop",
		StopReasonLength:  "length",
		StopReasonToolUse: "toolUse",
		StopReasonError:   "error",
		StopReasonAborted: "aborted",
	}
	for got, want := range cases {
		if string(got) != want {
			t.Errorf("%s = %q, want %q", want, got, want)
		}
	}
}

func TestThinkingLevelConstants(t *testing.T) {
	cases := map[ThinkingLevel]string{
		ThinkingOff:     "off",
		ThinkingMinimal: "minimal",
		ThinkingLow:     "low",
		ThinkingMedium:  "medium",
		ThinkingHigh:    "high",
		ThinkingXHigh:   "xhigh",
	}
	for got, want := range cases {
		if string(got) != want {
			t.Errorf("%s = %q, want %q", want, got, want)
		}
	}
}

func TestCacheRetentionConstants(t *testing.T) {
	cases := map[CacheRetention]string{
		CacheNone:  "none",
		CacheShort: "short",
		CacheLong:  "long",
	}
	for got, want := range cases {
		if string(got) != want {
			t.Errorf("%s = %q, want %q", want, got, want)
		}
	}
}

func TestEventTypeConstants(t *testing.T) {
	cases := map[EventType]string{
		EventStart:         "start",
		EventTextStart:     "text_start",
		EventTextDelta:     "text_delta",
		EventTextEnd:       "text_end",
		EventThinkingStart: "thinking_start",
		EventThinkingDelta: "thinking_delta",
		EventThinkingEnd:   "thinking_end",
		EventToolCallStart: "toolcall_start",
		EventToolCallDelta: "toolcall_delta",
		EventToolCallEnd:   "toolcall_end",
		EventDone:          "done",
		EventError:         "error",
	}
	for got, want := range cases {
		if string(got) != want {
			t.Errorf("%s = %q, want %q", want, got, want)
		}
	}
}

// ---------- Model round-trip ----------

func TestModelRoundTrip(t *testing.T) {
	orig := Model{
		ID:       "m-id",
		Name:     "My Model",
		Api:      ApiOpenAICompletions,
		Provider: ProviderOpenAI,
		BaseURL:  "https://api.openai.com/v1",
		Input:    []string{"text", "image"},
		Cost: ModelCost{
			Input: 3.0, Output: 15.0, CacheRead: 0.3, CacheWrite: 3.75,
		},
		ContextWindow: 200000,
		MaxTokens:     16384,
		Headers:       map[string]string{"x-custom": "1"},
	}
	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	var got Model
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(orig, got) {
		t.Errorf("mismatch:\norig=%+v\ngot =%+v", orig, got)
	}
}

// ---------- Marshal does not mutate caller ----------

func TestContentMarshalDoesNotMutate(t *testing.T) {
	c := &TextContent{Text: "hi"} // Type intentionally left empty
	if _, err := json.Marshal(c); err != nil {
		t.Fatal(err)
	}
	if c.Type != "" {
		t.Errorf("Type mutated to %q; MarshalJSON should not touch caller", c.Type)
	}
}

// ---------- Assistant marshal omits omitempty fields ----------

func TestAssistantMessageOmitsEmpty(t *testing.T) {
	m := &AssistantMessage{
		Content:    []Content{&TextContent{Text: "ok"}},
		Api:        ApiAnthropicMessages,
		Provider:   ProviderAnthropic,
		Model:      "m",
		Usage:      Usage{Input: 1, Output: 1, TotalTokens: 2},
		StopReason: StopReasonStop,
		Timestamp:  1,
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(s, "responseId") {
		t.Errorf("responseId should be omitted when empty: %s", s)
	}
	if strings.Contains(s, "errorMessage") {
		t.Errorf("errorMessage should be omitted when empty: %s", s)
	}
}
