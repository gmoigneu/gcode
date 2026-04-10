package ai

import (
	"context"
	"encoding/json"
	"fmt"
)

// Api identifies a wire-level LLM API protocol.
type Api string

const (
	ApiOpenAICompletions Api = "openai-completions"
	ApiAnthropicMessages Api = "anthropic-messages"
	ApiGoogleGemini      Api = "google-gemini"
)

// Provider identifies a specific LLM vendor or aggregator.
type Provider string

const (
	ProviderOpenAI     Provider = "openai"
	ProviderAnthropic  Provider = "anthropic"
	ProviderXAI        Provider = "xai"
	ProviderGroq       Provider = "groq"
	ProviderCerebras   Provider = "cerebras"
	ProviderOpenRouter Provider = "openrouter"
	ProviderGoogle     Provider = "google"
)

// Content is one block in a message. Concrete types are discriminated by the
// "type" field in JSON.
type Content interface {
	ContentType() string
}

// TextContent is a plain text block.
type TextContent struct {
	Type          string `json:"type"`
	Text          string `json:"text"`
	TextSignature string `json:"textSignature,omitempty"`
}

func (*TextContent) ContentType() string { return "text" }

func (c *TextContent) MarshalJSON() ([]byte, error) {
	type alias TextContent
	cp := alias(*c)
	cp.Type = "text"
	return json.Marshal(cp)
}

// ThinkingContent is a chain-of-thought block returned by reasoning models.
type ThinkingContent struct {
	Type              string `json:"type"`
	Thinking          string `json:"thinking"`
	ThinkingSignature string `json:"thinkingSignature,omitempty"`
	Redacted          bool   `json:"redacted,omitempty"`
}

func (*ThinkingContent) ContentType() string { return "thinking" }

func (c *ThinkingContent) MarshalJSON() ([]byte, error) {
	type alias ThinkingContent
	cp := alias(*c)
	cp.Type = "thinking"
	return json.Marshal(cp)
}

// ImageContent is a base64-encoded image block.
type ImageContent struct {
	Type     string `json:"type"`
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
}

func (*ImageContent) ContentType() string { return "image" }

func (c *ImageContent) MarshalJSON() ([]byte, error) {
	type alias ImageContent
	cp := alias(*c)
	cp.Type = "image"
	return json.Marshal(cp)
}

// ToolCall is a request from the assistant to invoke a tool.
type ToolCall struct {
	Type             string         `json:"type"`
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	Arguments        map[string]any `json:"arguments"`
	ThoughtSignature string         `json:"thoughtSignature,omitempty"`
}

func (*ToolCall) ContentType() string { return "toolCall" }

func (c *ToolCall) MarshalJSON() ([]byte, error) {
	type alias ToolCall
	cp := alias(*c)
	cp.Type = "toolCall"
	return json.Marshal(cp)
}

// UnmarshalContent decodes a single Content block, picking the concrete type
// from the "type" discriminator field.
func UnmarshalContent(data []byte) (Content, error) {
	var peek struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &peek); err != nil {
		return nil, fmt.Errorf("ai: content: decode discriminator: %w", err)
	}
	if peek.Type == "" {
		return nil, fmt.Errorf("ai: content: missing type field")
	}
	switch peek.Type {
	case "text":
		var c TextContent
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, fmt.Errorf("ai: content: decode text: %w", err)
		}
		return &c, nil
	case "thinking":
		var c ThinkingContent
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, fmt.Errorf("ai: content: decode thinking: %w", err)
		}
		return &c, nil
	case "image":
		var c ImageContent
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, fmt.Errorf("ai: content: decode image: %w", err)
		}
		return &c, nil
	case "toolCall":
		var c ToolCall
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, fmt.Errorf("ai: content: decode toolCall: %w", err)
		}
		return &c, nil
	default:
		return nil, fmt.Errorf("ai: content: unknown type %q", peek.Type)
	}
}

// unmarshalContentSlice decodes a JSON array of content blocks.
func unmarshalContentSlice(data []byte) ([]Content, error) {
	if len(data) == 0 || string(data) == "null" {
		return nil, nil
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(data, &raws); err != nil {
		return nil, fmt.Errorf("ai: content: decode array: %w", err)
	}
	out := make([]Content, 0, len(raws))
	for i, raw := range raws {
		c, err := UnmarshalContent(raw)
		if err != nil {
			return nil, fmt.Errorf("ai: content[%d]: %w", i, err)
		}
		out = append(out, c)
	}
	return out, nil
}

// Message is a turn in a conversation. Concrete types are discriminated by the
// "role" field in JSON.
type Message interface {
	MessageRole() string
	MessageTimestamp() int64
}

// UserMessage is input from the human or tool-wielding caller.
type UserMessage struct {
	Content   []Content `json:"content"`
	Timestamp int64     `json:"timestamp"`
}

func (*UserMessage) MessageRole() string       { return "user" }
func (m *UserMessage) MessageTimestamp() int64 { return m.Timestamp }

func (m *UserMessage) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Role      string    `json:"role"`
		Content   []Content `json:"content"`
		Timestamp int64     `json:"timestamp"`
	}{
		Role:      "user",
		Content:   m.Content,
		Timestamp: m.Timestamp,
	})
}

func (m *UserMessage) UnmarshalJSON(data []byte) error {
	var wire struct {
		Role      string          `json:"role"`
		Content   json.RawMessage `json:"content"`
		Timestamp int64           `json:"timestamp"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return fmt.Errorf("ai: userMessage: %w", err)
	}
	content, err := unmarshalContentSlice(wire.Content)
	if err != nil {
		return fmt.Errorf("ai: userMessage: %w", err)
	}
	m.Content = content
	m.Timestamp = wire.Timestamp
	return nil
}

// AssistantMessage is a completed model response.
type AssistantMessage struct {
	Content      []Content  `json:"content"`
	Api          Api        `json:"api"`
	Provider     Provider   `json:"provider"`
	Model        string     `json:"model"`
	ResponseID   string     `json:"responseId,omitempty"`
	Usage        Usage      `json:"usage"`
	StopReason   StopReason `json:"stopReason"`
	ErrorMessage string     `json:"errorMessage,omitempty"`
	Timestamp    int64      `json:"timestamp"`
}

func (*AssistantMessage) MessageRole() string       { return "assistant" }
func (m *AssistantMessage) MessageTimestamp() int64 { return m.Timestamp }

func (m *AssistantMessage) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Role         string     `json:"role"`
		Content      []Content  `json:"content"`
		Api          Api        `json:"api"`
		Provider     Provider   `json:"provider"`
		Model        string     `json:"model"`
		ResponseID   string     `json:"responseId,omitempty"`
		Usage        Usage      `json:"usage"`
		StopReason   StopReason `json:"stopReason"`
		ErrorMessage string     `json:"errorMessage,omitempty"`
		Timestamp    int64      `json:"timestamp"`
	}{
		Role:         "assistant",
		Content:      m.Content,
		Api:          m.Api,
		Provider:     m.Provider,
		Model:        m.Model,
		ResponseID:   m.ResponseID,
		Usage:        m.Usage,
		StopReason:   m.StopReason,
		ErrorMessage: m.ErrorMessage,
		Timestamp:    m.Timestamp,
	})
}

func (m *AssistantMessage) UnmarshalJSON(data []byte) error {
	var wire struct {
		Role         string          `json:"role"`
		Content      json.RawMessage `json:"content"`
		Api          Api             `json:"api"`
		Provider     Provider        `json:"provider"`
		Model        string          `json:"model"`
		ResponseID   string          `json:"responseId,omitempty"`
		Usage        Usage           `json:"usage"`
		StopReason   StopReason      `json:"stopReason"`
		ErrorMessage string          `json:"errorMessage,omitempty"`
		Timestamp    int64           `json:"timestamp"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return fmt.Errorf("ai: assistantMessage: %w", err)
	}
	content, err := unmarshalContentSlice(wire.Content)
	if err != nil {
		return fmt.Errorf("ai: assistantMessage: %w", err)
	}
	m.Content = content
	m.Api = wire.Api
	m.Provider = wire.Provider
	m.Model = wire.Model
	m.ResponseID = wire.ResponseID
	m.Usage = wire.Usage
	m.StopReason = wire.StopReason
	m.ErrorMessage = wire.ErrorMessage
	m.Timestamp = wire.Timestamp
	return nil
}

// ToolResultMessage is the result of executing a tool, returned to the model.
type ToolResultMessage struct {
	ToolCallID string    `json:"toolCallId"`
	ToolName   string    `json:"toolName"`
	Content    []Content `json:"content"`
	Details    any       `json:"details,omitempty"`
	IsError    bool      `json:"isError"`
	Timestamp  int64     `json:"timestamp"`
}

func (*ToolResultMessage) MessageRole() string       { return "toolResult" }
func (m *ToolResultMessage) MessageTimestamp() int64 { return m.Timestamp }

func (m *ToolResultMessage) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Role       string    `json:"role"`
		ToolCallID string    `json:"toolCallId"`
		ToolName   string    `json:"toolName"`
		Content    []Content `json:"content"`
		Details    any       `json:"details,omitempty"`
		IsError    bool      `json:"isError"`
		Timestamp  int64     `json:"timestamp"`
	}{
		Role:       "toolResult",
		ToolCallID: m.ToolCallID,
		ToolName:   m.ToolName,
		Content:    m.Content,
		Details:    m.Details,
		IsError:    m.IsError,
		Timestamp:  m.Timestamp,
	})
}

func (m *ToolResultMessage) UnmarshalJSON(data []byte) error {
	var wire struct {
		Role       string          `json:"role"`
		ToolCallID string          `json:"toolCallId"`
		ToolName   string          `json:"toolName"`
		Content    json.RawMessage `json:"content"`
		Details    json.RawMessage `json:"details,omitempty"`
		IsError    bool            `json:"isError"`
		Timestamp  int64           `json:"timestamp"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return fmt.Errorf("ai: toolResultMessage: %w", err)
	}
	content, err := unmarshalContentSlice(wire.Content)
	if err != nil {
		return fmt.Errorf("ai: toolResultMessage: %w", err)
	}
	m.ToolCallID = wire.ToolCallID
	m.ToolName = wire.ToolName
	m.Content = content
	if len(wire.Details) > 0 && string(wire.Details) != "null" {
		var d any
		if err := json.Unmarshal(wire.Details, &d); err != nil {
			return fmt.Errorf("ai: toolResultMessage: details: %w", err)
		}
		m.Details = d
	}
	m.IsError = wire.IsError
	m.Timestamp = wire.Timestamp
	return nil
}

// UnmarshalMessage decodes a single Message, picking the concrete type from
// the "role" discriminator field.
func UnmarshalMessage(data []byte) (Message, error) {
	var peek struct {
		Role string `json:"role"`
	}
	if err := json.Unmarshal(data, &peek); err != nil {
		return nil, fmt.Errorf("ai: message: decode discriminator: %w", err)
	}
	if peek.Role == "" {
		return nil, fmt.Errorf("ai: message: missing role field")
	}
	switch peek.Role {
	case "user":
		var m UserMessage
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		return &m, nil
	case "assistant":
		var m AssistantMessage
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		return &m, nil
	case "toolResult":
		var m ToolResultMessage
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		return &m, nil
	default:
		return nil, fmt.Errorf("ai: message: unknown role %q", peek.Role)
	}
}

// Usage reports token consumption and derived cost for an assistant response.
type Usage struct {
	Input       int  `json:"input"`
	Output      int  `json:"output"`
	CacheRead   int  `json:"cacheRead"`
	CacheWrite  int  `json:"cacheWrite"`
	TotalTokens int  `json:"totalTokens"`
	Cost        Cost `json:"cost"`
}

// Cost is the monetary cost of a single response, in USD.
type Cost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
	Total      float64 `json:"total"`
}

// StopReason explains why the model stopped generating.
type StopReason string

const (
	StopReasonStop    StopReason = "stop"
	StopReasonLength  StopReason = "length"
	StopReasonToolUse StopReason = "toolUse"
	StopReasonError   StopReason = "error"
	StopReasonAborted StopReason = "aborted"
)

// Tool describes a callable tool exposed to the model.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// Context is a full prompt: system instruction, conversation, and available tools.
type Context struct {
	SystemPrompt string    `json:"systemPrompt,omitempty"`
	Messages     []Message `json:"messages"`
	Tools        []Tool    `json:"tools,omitempty"`
}

func (c *Context) UnmarshalJSON(data []byte) error {
	var wire struct {
		SystemPrompt string            `json:"systemPrompt,omitempty"`
		Messages     []json.RawMessage `json:"messages"`
		Tools        []Tool            `json:"tools,omitempty"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return fmt.Errorf("ai: context: %w", err)
	}
	c.SystemPrompt = wire.SystemPrompt
	c.Tools = wire.Tools
	c.Messages = nil
	for i, raw := range wire.Messages {
		m, err := UnmarshalMessage(raw)
		if err != nil {
			return fmt.Errorf("ai: context: messages[%d]: %w", i, err)
		}
		c.Messages = append(c.Messages, m)
	}
	return nil
}

// Model describes an LLM configured for gcode.
type Model struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Api           Api               `json:"api"`
	Provider      Provider          `json:"provider"`
	BaseURL       string            `json:"baseUrl"`
	Reasoning     bool              `json:"reasoning"`
	Input         []string          `json:"input"`
	Cost          ModelCost         `json:"cost"`
	ContextWindow int               `json:"contextWindow"`
	MaxTokens     int               `json:"maxTokens"`
	Headers       map[string]string `json:"headers,omitempty"`
	Compat        *OpenAICompat     `json:"compat,omitempty"`
}

// ModelCost is the per-million-token pricing for a model, in USD.
type ModelCost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
}

// OpenAICompat captures per-model quirks for OpenAI-compatible providers.
// The concrete field set is filled in by the providers subpackage; this type
// lives here so Model can reference it without a cyclic import.
type OpenAICompat struct {
	SupportsDeveloperRole    bool                     `json:"supportsDeveloperRole,omitempty"`
	SupportsReasoningEffort  bool                     `json:"supportsReasoningEffort,omitempty"`
	ReasoningEffortMap       map[ThinkingLevel]string `json:"reasoningEffortMap,omitempty"`
	SupportsUsageInStreaming bool                     `json:"supportsUsageInStreaming,omitempty"`
	MaxTokensField           string                   `json:"maxTokensField,omitempty"`
	RequiresToolResultName   bool                     `json:"requiresToolResultName,omitempty"`
	RequiresThinkingAsText   bool                     `json:"requiresThinkingAsText,omitempty"`
	ThinkingFormat           string                   `json:"thinkingFormat,omitempty"`
	SupportsStrictMode       bool                     `json:"supportsStrictMode,omitempty"`
}

// StreamOptions are the per-request knobs shared by all providers.
// Signal carries cancellation and is not serialized.
type StreamOptions struct {
	Temperature    *float64          `json:"temperature,omitempty"`
	MaxTokens      *int              `json:"maxTokens,omitempty"`
	Signal         context.Context   `json:"-"`
	APIKey         string            `json:"apiKey,omitempty"`
	CacheRetention string            `json:"cacheRetention,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
}

// SimpleStreamOptions extends StreamOptions with reasoning controls for the
// simplified streaming API.
type SimpleStreamOptions struct {
	StreamOptions
	Reasoning       ThinkingLevel    `json:"reasoning,omitempty"`
	ThinkingBudgets *ThinkingBudgets `json:"thinkingBudgets,omitempty"`
}

// ThinkingLevel is a coarse reasoning effort dial.
type ThinkingLevel string

const (
	ThinkingOff     ThinkingLevel = "off"
	ThinkingMinimal ThinkingLevel = "minimal"
	ThinkingLow     ThinkingLevel = "low"
	ThinkingMedium  ThinkingLevel = "medium"
	ThinkingHigh    ThinkingLevel = "high"
	ThinkingXHigh   ThinkingLevel = "xhigh"
)

// ThinkingBudgets maps each ThinkingLevel to an explicit token budget.
type ThinkingBudgets struct {
	Minimal int `json:"minimal,omitempty"`
	Low     int `json:"low,omitempty"`
	Medium  int `json:"medium,omitempty"`
	High    int `json:"high,omitempty"`
}

// CacheRetention selects a prompt-cache TTL tier.
type CacheRetention string

const (
	CacheNone  CacheRetention = "none"
	CacheShort CacheRetention = "short"
	CacheLong  CacheRetention = "long"
)

// EventType is the kind tag on an AssistantMessageEvent.
type EventType string

const (
	EventStart         EventType = "start"
	EventTextStart     EventType = "text_start"
	EventTextDelta     EventType = "text_delta"
	EventTextEnd       EventType = "text_end"
	EventThinkingStart EventType = "thinking_start"
	EventThinkingDelta EventType = "thinking_delta"
	EventThinkingEnd   EventType = "thinking_end"
	EventToolCallStart EventType = "toolcall_start"
	EventToolCallDelta EventType = "toolcall_delta"
	EventToolCallEnd   EventType = "toolcall_end"
	EventDone          EventType = "done"
	EventError         EventType = "error"
)

// AssistantMessageEvent is a single element of a streaming response.
// Fields are populated based on Type; see EventType for the contract.
type AssistantMessageEvent struct {
	Type         EventType
	ContentIndex int
	Delta        string
	Content      string
	ToolCall     *ToolCall
	Reason       StopReason
	Partial      *AssistantMessage
	Message      *AssistantMessage
	Error        *AssistantMessage
}
