package providers

import "encoding/json"

// openAIMessage is one entry in the chat/completions request messages array.
// Content may be a plain string (for simple text) or a []openAIPart for
// multi-modal content.
type openAIMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content,omitempty"`
	Name       string           `json:"name,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAIPart struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	ImageURL *openAIImageURL `json:"image_url,omitempty"`
}

type openAIImageURL struct {
	URL string `json:"url"`
}

type openAIToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function openAIToolCallFn `json:"function"`
}

type openAIToolCallFn struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAITool struct {
	Type     string         `json:"type"`
	Function openAIFnSchema `json:"function"`
}

type openAIFnSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
	Strict      bool            `json:"strict,omitempty"`
}

type openAIStreamOpts struct {
	IncludeUsage bool `json:"include_usage"`
}

// openAIRequest is marshalled directly. The MaxTokensField on OpenAICompat
// selects between "max_tokens" and "max_completion_tokens", so we use a
// custom encoder (see buildOpenAIRequest).
type openAIRequest struct {
	Model           string            `json:"model"`
	Messages        []openAIMessage   `json:"messages"`
	Stream          bool              `json:"stream"`
	Temperature     *float64          `json:"temperature,omitempty"`
	MaxTokens       *int              `json:"-"`
	Tools           []openAITool      `json:"tools,omitempty"`
	StreamOptions   *openAIStreamOpts `json:"stream_options,omitempty"`
	ReasoningEffort string            `json:"reasoning_effort,omitempty"`

	// MaxTokensField is rendered via custom marshal (either "max_tokens" or
	// "max_completion_tokens").
	MaxTokensField string `json:"-"`
}

// MarshalJSON renders the request, injecting MaxTokens under the right field
// name.
func (r openAIRequest) MarshalJSON() ([]byte, error) {
	type alias openAIRequest
	base, err := json.Marshal(alias(r))
	if err != nil {
		return nil, err
	}
	if r.MaxTokens == nil {
		return base, nil
	}
	// base ends with '}'
	field := r.MaxTokensField
	if field == "" {
		field = "max_tokens"
	}
	injected := map[string]any{field: *r.MaxTokens}
	extra, err := json.Marshal(injected)
	if err != nil {
		return nil, err
	}
	// Insert the extra field before the closing brace.
	if len(base) < 2 || base[len(base)-1] != '}' {
		return base, nil
	}
	out := make([]byte, 0, len(base)+len(extra))
	out = append(out, base[:len(base)-1]...)
	if len(base) > 2 { // non-empty object
		out = append(out, ',')
	}
	out = append(out, extra[1:len(extra)-1]...) // strip the surrounding {}
	out = append(out, '}')
	return out, nil
}

// ----- response/stream wire types -----

type openAIChunk struct {
	ID      string         `json:"id"`
	Choices []openAIChoice `json:"choices"`
	Usage   *openAIUsage   `json:"usage,omitempty"`
}

type openAIChoice struct {
	Index        int         `json:"index"`
	Delta        openAIDelta `json:"delta"`
	FinishReason string      `json:"finish_reason,omitempty"`
}

type openAIDelta struct {
	Role             string                `json:"role,omitempty"`
	Content          string                `json:"content,omitempty"`
	ReasoningContent string                `json:"reasoning_content,omitempty"`
	Reasoning        string                `json:"reasoning,omitempty"`
	ReasoningText    string                `json:"reasoning_text,omitempty"`
	ToolCalls        []openAIToolCallDelta `json:"tool_calls,omitempty"`
}

type openAIToolCallDelta struct {
	Index    *int           `json:"index,omitempty"`
	ID       string         `json:"id,omitempty"`
	Type     string         `json:"type,omitempty"`
	Function *openAIFnDelta `json:"function,omitempty"`
}

type openAIFnDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type openAIUsage struct {
	PromptTokens        int                        `json:"prompt_tokens"`
	CompletionTokens    int                        `json:"completion_tokens"`
	TotalTokens         int                        `json:"total_tokens"`
	PromptTokensDetails *openAIPromptTokensDetails `json:"prompt_tokens_details,omitempty"`
}

type openAIPromptTokensDetails struct {
	CachedTokens     int `json:"cached_tokens"`
	CacheWriteTokens int `json:"cache_write_tokens"`
}
