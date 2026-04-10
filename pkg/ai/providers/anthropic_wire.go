package providers

import "encoding/json"

// anthropicRequest mirrors the POST /v1/messages body.
type anthropicRequest struct {
	Model        string              `json:"model"`
	MaxTokens    int                 `json:"max_tokens"`
	System       []anthropicSysBlock `json:"system,omitempty"`
	Messages     []anthropicMessage  `json:"messages"`
	Tools        []anthropicTool     `json:"tools,omitempty"`
	Temperature  *float64            `json:"temperature,omitempty"`
	Thinking     *anthropicThinking  `json:"thinking,omitempty"`
	OutputConfig *anthropicOutputCfg `json:"output_config,omitempty"`
	Stream       bool                `json:"stream"`
}

type anthropicSysBlock struct {
	Type         string         `json:"type"`
	Text         string         `json:"text"`
	CacheControl map[string]any `json:"cache_control,omitempty"`
}

type anthropicMessage struct {
	Role    string             `json:"role"` // "user" or "assistant"
	Content []anthropicContent `json:"content"`
}

type anthropicContent struct {
	Type         string          `json:"type"`
	Text         string          `json:"text,omitempty"`
	Source       *anthropicImage `json:"source,omitempty"`
	ID           string          `json:"id,omitempty"`
	Name         string          `json:"name,omitempty"`
	Input        any             `json:"input,omitempty"`
	ToolUseID    string          `json:"tool_use_id,omitempty"`
	Content      any             `json:"content,omitempty"`
	Thinking     string          `json:"thinking,omitempty"`
	Signature    string          `json:"signature,omitempty"`
	CacheControl map[string]any  `json:"cache_control,omitempty"`
}

type anthropicImage struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthropicThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

type anthropicOutputCfg struct {
	Effort string `json:"effort,omitempty"`
}

// ----- streaming wire types -----

type anthropicStartEnvelope struct {
	Type    string                 `json:"type"`
	Message *anthropicStartMessage `json:"message,omitempty"`
	Index   int                    `json:"index,omitempty"`
	Block   *anthropicBlockStart   `json:"content_block,omitempty"`
	Delta   *anthropicBlockDelta   `json:"delta,omitempty"`
	Usage   *anthropicUsage        `json:"usage,omitempty"`
}

type anthropicStartMessage struct {
	ID         string          `json:"id"`
	Role       string          `json:"role"`
	Model      string          `json:"model"`
	StopReason string          `json:"stop_reason"`
	Usage      *anthropicUsage `json:"usage,omitempty"`
}

type anthropicBlockStart struct {
	Type  string `json:"type"` // text | thinking | tool_use | redacted_thinking
	Text  string `json:"text,omitempty"`
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Input any    `json:"input,omitempty"`
}

type anthropicBlockDelta struct {
	Type         string `json:"type"`
	Text         string `json:"text,omitempty"`
	Thinking     string `json:"thinking,omitempty"`
	PartialJSON  string `json:"partial_json,omitempty"`
	StopReason   string `json:"stop_reason,omitempty"`
	StopSequence string `json:"stop_sequence,omitempty"`
}

type anthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}
