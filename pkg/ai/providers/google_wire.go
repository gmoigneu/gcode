package providers

import "encoding/json"

type googleRequest struct {
	Contents          []googleContent     `json:"contents"`
	SystemInstruction *googleSystem       `json:"system_instruction,omitempty"`
	Tools             []googleToolWrapper `json:"tools,omitempty"`
	GenerationConfig  *googleGenConfig    `json:"generation_config,omitempty"`
	ThinkingConfig    *googleThinkingCfg  `json:"thinking_config,omitempty"`
}

type googleSystem struct {
	Parts []googlePart `json:"parts"`
}

type googleContent struct {
	Role  string       `json:"role"`
	Parts []googlePart `json:"parts"`
}

type googlePart struct {
	Text             string                  `json:"text,omitempty"`
	InlineData       *googleInlineData       `json:"inline_data,omitempty"`
	FunctionCall     *googleFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *googleFunctionResponse `json:"functionResponse,omitempty"`
}

type googleInlineData struct {
	MimeType string `json:"mime_type"`
	Data     string `json:"data"`
}

type googleFunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type googleFunctionResponse struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type googleToolWrapper struct {
	FunctionDeclarations []googleFunctionDecl `json:"function_declarations"`
}

type googleFunctionDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type googleGenConfig struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	MaxOutputTokens *int     `json:"max_output_tokens,omitempty"`
}

type googleThinkingCfg struct {
	ThinkingBudget  int  `json:"thinking_budget,omitempty"`
	IncludeThoughts bool `json:"include_thoughts,omitempty"`
}

// ----- streaming response -----

type googleStreamChunk struct {
	Candidates    []googleCandidate `json:"candidates"`
	UsageMetadata *googleUsage      `json:"usageMetadata,omitempty"`
}

type googleCandidate struct {
	Content      *googleContent `json:"content"`
	FinishReason string         `json:"finishReason,omitempty"`
}

type googleUsage struct {
	PromptTokenCount        int `json:"promptTokenCount"`
	CandidatesTokenCount    int `json:"candidatesTokenCount"`
	TotalTokenCount         int `json:"totalTokenCount"`
	CachedContentTokenCount int `json:"cachedContentTokenCount"`
}
