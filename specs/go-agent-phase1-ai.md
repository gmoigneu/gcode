# Phase 1: LLM streaming abstraction (`pkg/ai`)

> **Project**: gcode (not pi). Go 1.24+. Raw HTTP only, no vendor SDKs. All LLM communication uses `net/http` and manual JSON marshaling.

The foundation. All LLM communication flows through this package. No internal dependencies.

## 1.1 Core types (`pkg/ai/types.go`)

### API and provider identifiers

```go
type Api string

const (
    ApiOpenAICompletions Api = "openai-completions"
    ApiAnthropicMessages Api = "anthropic-messages"
    ApiGoogleGemini     Api = "google-gemini"
)

type Provider string

const (
    ProviderOpenAI    Provider = "openai"
    ProviderAnthropic Provider = "anthropic"
    ProviderXAI       Provider = "xai"
    ProviderGroq      Provider = "groq"
    ProviderCerebras  Provider = "cerebras"
    ProviderOpenRouter Provider = "openrouter"
    ProviderGoogle    Provider = "google"
)
```

### Content types

```go
type TextContent struct {
    Type           string `json:"type"` // always "text"
    Text           string `json:"text"`
    TextSignature  string `json:"textSignature,omitempty"`
}

type ThinkingContent struct {
    Type              string `json:"type"` // always "thinking"
    Thinking          string `json:"thinking"`
    ThinkingSignature string `json:"thinkingSignature,omitempty"`
    Redacted          bool   `json:"redacted,omitempty"`
}

type ImageContent struct {
    Type     string `json:"type"` // always "image"
    Data     string `json:"data"` // base64
    MimeType string `json:"mimeType"`
}

type ToolCall struct {
    Type             string         `json:"type"` // always "toolCall"
    ID               string         `json:"id"`
    Name             string         `json:"name"`
    Arguments        map[string]any `json:"arguments"`
    ThoughtSignature string         `json:"thoughtSignature,omitempty"`
}
```

Use a `Content` interface to unify these:

```go
type Content interface {
    ContentType() string
}

// TextContent, ThinkingContent, ImageContent, ToolCall all implement Content
```

### Messages

```go
type UserMessage struct {
    Role      string    `json:"role"` // "user"
    Content   []Content `json:"content"`
    Timestamp int64     `json:"timestamp"` // Unix ms
}

type AssistantMessage struct {
    Role         string    `json:"role"` // "assistant"
    Content      []Content `json:"content"`
    Api          Api       `json:"api"`
    Provider     Provider  `json:"provider"`
    Model        string    `json:"model"`
    ResponseID   string    `json:"responseId,omitempty"`
    Usage        Usage     `json:"usage"`
    StopReason   StopReason `json:"stopReason"`
    ErrorMessage string    `json:"errorMessage,omitempty"`
    Timestamp    int64     `json:"timestamp"`
}

type ToolResultMessage struct {
    Role       string    `json:"role"` // "toolResult"
    ToolCallID string    `json:"toolCallId"`
    ToolName   string    `json:"toolName"`
    Content    []Content `json:"content"`
    Details    any       `json:"details,omitempty"`
    IsError    bool      `json:"isError"`
    Timestamp  int64     `json:"timestamp"`
}

// Message is the union. Use an interface:
type Message interface {
    MessageRole() string
    MessageTimestamp() int64
}
```

**Custom JSON marshaling**: Since Go doesn't have discriminated unions, implement custom `MarshalJSON`/`UnmarshalJSON` on the `Content` slice and `Message` types. Use the `type` or `role` field as discriminator.

### Usage and cost

```go
type Usage struct {
    Input      int  `json:"input"`
    Output     int  `json:"output"`
    CacheRead  int  `json:"cacheRead"`
    CacheWrite int  `json:"cacheWrite"`
    TotalTokens int `json:"totalTokens"`
    Cost       Cost `json:"cost"`
}

type Cost struct {
    Input      float64 `json:"input"`
    Output     float64 `json:"output"`
    CacheRead  float64 `json:"cacheRead"`
    CacheWrite float64 `json:"cacheWrite"`
    Total      float64 `json:"total"`
}

type StopReason string

const (
    StopReasonStop    StopReason = "stop"
    StopReasonLength  StopReason = "length"
    StopReasonToolUse StopReason = "toolUse"
    StopReasonError   StopReason = "error"
    StopReasonAborted StopReason = "aborted"
)
```

### Tools

```go
type Tool struct {
    Name        string          `json:"name"`
    Description string          `json:"description"`
    Parameters  json.RawMessage `json:"parameters"` // JSON Schema
}
```

`json.RawMessage` for parameters because Go doesn't have TypeBox. Tools register with a JSON Schema blob. Use a helper to generate schemas from Go structs (see section 1.5).

### Context

```go
type Context struct {
    SystemPrompt string    `json:"systemPrompt,omitempty"`
    Messages     []Message `json:"messages"`
    Tools        []Tool    `json:"tools,omitempty"`
}
```

### Model

```go
type Model struct {
    ID            string            `json:"id"`
    Name          string            `json:"name"`
    Api           Api               `json:"api"`
    Provider      Provider          `json:"provider"`
    BaseURL       string            `json:"baseUrl"`
    Reasoning     bool              `json:"reasoning"`
    Input         []string          `json:"input"` // "text", "image"
    Cost          ModelCost         `json:"cost"`
    ContextWindow int               `json:"contextWindow"`
    MaxTokens     int               `json:"maxTokens"`
    Headers       map[string]string `json:"headers,omitempty"`
    Compat        *OpenAICompat     `json:"compat,omitempty"` // only for openai-completions
}

type ModelCost struct {
    Input      float64 `json:"input"`      // $ per million tokens
    Output     float64 `json:"output"`
    CacheRead  float64 `json:"cacheRead"`
    CacheWrite float64 `json:"cacheWrite"`
}
```

### Stream options

```go
type StreamOptions struct {
    Temperature    *float64          `json:"temperature,omitempty"`
    MaxTokens      *int              `json:"maxTokens,omitempty"`
    Signal         context.Context   // Go uses context.Context for cancellation
    APIKey         string            `json:"apiKey,omitempty"`
    CacheRetention string            `json:"cacheRetention,omitempty"` // "none"|"short"|"long"
    Headers        map[string]string `json:"headers,omitempty"`
}

type SimpleStreamOptions struct {
    StreamOptions
    Reasoning      ThinkingLevel     `json:"reasoning,omitempty"`
    ThinkingBudgets *ThinkingBudgets `json:"thinkingBudgets,omitempty"`
}

type ThinkingLevel string

const (
    ThinkingOff     ThinkingLevel = "off"
    ThinkingMinimal ThinkingLevel = "minimal"
    ThinkingLow     ThinkingLevel = "low"
    ThinkingMedium  ThinkingLevel = "medium"
    ThinkingHigh    ThinkingLevel = "high"
    ThinkingXHigh   ThinkingLevel = "xhigh"
)

type ThinkingBudgets struct {
    Minimal int `json:"minimal,omitempty"` // default 1024
    Low     int `json:"low,omitempty"`     // default 2048
    Medium  int `json:"medium,omitempty"`  // default 8192
    High    int `json:"high,omitempty"`    // default 16384
}
```

### Streaming events

```go
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

type AssistantMessageEvent struct {
    Type         EventType
    ContentIndex int              // index into partial.Content[]
    Delta        string           // for *_delta events
    Content      string           // for *_end events (full accumulated text)
    ToolCall     *ToolCall         // for toolcall_end
    Reason       StopReason        // for done/error
    Partial      *AssistantMessage // in-progress message (all non-terminal events)
    Message      *AssistantMessage // final message (done event)
    Error        *AssistantMessage // error message (error event)
}
```

Single struct with optional fields rather than a Go interface hierarchy. Simpler to work with in switch statements.

## 1.2 Event stream (`pkg/ai/event_stream.go`)

The Go equivalent of pi's `EventStream<T, R>`. Uses channels instead of async iterables.

```go
type AssistantMessageEventStream struct {
    C      <-chan AssistantMessageEvent // Public read-only channel
    ch     chan AssistantMessageEvent   // Internal write channel
    result chan AssistantMessage        // Buffered(1), receives final message
    done   chan struct{}               // Closed when stream ends
    once   sync.Once                   // Ensures end() is called only once
}

func NewAssistantMessageEventStream() *AssistantMessageEventStream {
    ch := make(chan AssistantMessageEvent, 64) // buffered to prevent producer blocking
    return &AssistantMessageEventStream{
        C:      ch,
        ch:     ch,
        result: make(chan AssistantMessage, 1),
        done:   make(chan struct{}),
    }
}

// Push sends an event to consumers. Safe to call from any goroutine.
// If the event is terminal (done/error), it also resolves the result.
func (s *AssistantMessageEventStream) Push(event AssistantMessageEvent) {
    select {
    case <-s.done:
        return // stream already ended, drop
    default:
    }

    s.ch <- event

    if event.Type == EventDone || event.Type == EventError {
        s.End(event)
    }
}

// End closes the stream. Safe to call multiple times.
func (s *AssistantMessageEventStream) End(finalEvent AssistantMessageEvent) {
    s.once.Do(func() {
        if finalEvent.Type == EventDone && finalEvent.Message != nil {
            s.result <- *finalEvent.Message
        } else if finalEvent.Type == EventError && finalEvent.Error != nil {
            s.result <- *finalEvent.Error
        }
        close(s.done)
        close(s.ch)
    })
}

// Result blocks until the stream completes and returns the final AssistantMessage.
func (s *AssistantMessageEventStream) Result() AssistantMessage {
    return <-s.result
}

// Done returns a channel that's closed when the stream ends.
func (s *AssistantMessageEventStream) Done() <-chan struct{} {
    return s.done
}
```

**Usage pattern** (consumer):
```go
stream := ai.Stream(model, ctx, opts)
for event := range stream.C {
    switch event.Type {
    case ai.EventTextDelta:
        fmt.Print(event.Delta)
    case ai.EventDone:
        // stream.C will close after this
    case ai.EventError:
        log.Error(event.Error.ErrorMessage)
    }
}
```

**Usage pattern** (producer / provider):
```go
func streamAnthropic(model Model, ctx Context, opts *StreamOptions) *AssistantMessageEventStream {
    stream := NewAssistantMessageEventStream()
    go func() {
        // ... HTTP request, SSE parsing, push events ...
        // stream.Push(event) for each SSE chunk
        // stream.Push(doneEvent) at the end (this also calls End())
    }()
    return stream
}
```

## 1.3 Provider registry (`pkg/ai/registry.go`)

```go
type StreamFunc func(model Model, ctx Context, opts *StreamOptions) *AssistantMessageEventStream
type SimpleStreamFunc func(model Model, ctx Context, opts *SimpleStreamOptions) *AssistantMessageEventStream

type ApiProvider struct {
    Api          Api
    Stream       StreamFunc
    StreamSimple SimpleStreamFunc
}

var (
    mu        sync.RWMutex
    providers = make(map[Api]*ApiProvider)
)

func RegisterProvider(p *ApiProvider) {
    mu.Lock()
    defer mu.Unlock()
    providers[p.Api] = p
}

func GetProvider(api Api) (*ApiProvider, bool) {
    mu.RLock()
    defer mu.RUnlock()
    p, ok := providers[api]
    return p, ok
}
```

**No lazy loading needed in Go.** Go's `init()` functions handle registration at program startup. Each provider file has:

```go
func init() {
    RegisterProvider(&ApiProvider{
        Api:          ApiOpenAICompletions,
        Stream:       streamOpenAICompletions,
        StreamSimple: streamSimpleOpenAICompletions,
    })
}
```

If binary size becomes a concern, use build tags to exclude providers:
```go
//go:build !noopenai
```

## 1.4 Public API (`pkg/ai/stream.go`)

```go
func Stream(model Model, ctx Context, opts *StreamOptions) *AssistantMessageEventStream {
    p, ok := GetProvider(model.Api)
    if !ok {
        return errorStream(fmt.Errorf("no provider for api: %s", model.Api))
    }
    return p.Stream(model, ctx, opts)
}

func StreamSimple(model Model, ctx Context, opts *SimpleStreamOptions) *AssistantMessageEventStream {
    p, ok := GetProvider(model.Api)
    if !ok {
        return errorStream(fmt.Errorf("no provider for api: %s", model.Api))
    }
    return p.StreamSimple(model, ctx, opts)
}

func Complete(model Model, ctx Context, opts *StreamOptions) AssistantMessage {
    return Stream(model, ctx, opts).Result()
}

func CompleteSimple(model Model, ctx Context, opts *SimpleStreamOptions) AssistantMessage {
    return StreamSimple(model, ctx, opts).Result()
}

// errorStream returns a stream that immediately emits an error event.
func errorStream(err error) *AssistantMessageEventStream {
    stream := NewAssistantMessageEventStream()
    go func() {
        errMsg := &AssistantMessage{
            Role:         "assistant",
            StopReason:   StopReasonError,
            ErrorMessage: err.Error(),
            Timestamp:    time.Now().UnixMilli(),
        }
        stream.Push(AssistantMessageEvent{
            Type:   EventError,
            Reason: StopReasonError,
            Error:  errMsg,
        })
    }()
    return stream
}
```

## 1.5 JSON Schema generation (`pkg/ai/schema.go`)

Lightweight helper to generate JSON Schema from Go structs using reflection. Not a full implementation, just enough for tool parameters.

```go
// SchemaFrom generates a JSON Schema from a Go struct type.
// Supports: string, int, float64, bool, []T, *T (optional), struct.
// Uses json tags for field names and `description` tags for descriptions.
func SchemaFrom[T any]() json.RawMessage

// Example:
type ReadParams struct {
    Path   string `json:"path" description:"Path to the file to read"`
    Offset *int   `json:"offset,omitempty" description:"Line number to start reading from (1-indexed)"`
    Limit  *int   `json:"limit,omitempty" description:"Maximum number of lines to read"`
}

schema := ai.SchemaFrom[ReadParams]()
// Produces: {"type":"object","properties":{"path":{"type":"string","description":"..."},...},"required":["path"]}
```

Implementation: use `reflect.TypeOf`, iterate fields, map Go types to JSON Schema types, handle `omitempty` for optional fields (not in `required`), handle pointer types as optional.

## 1.6 Streaming JSON parser (`pkg/ai/json_parse.go`)

Tool call arguments arrive as partial JSON during streaming. Need a parser that handles incomplete JSON gracefully.

```go
// ParseStreamingJSON attempts to parse potentially incomplete JSON.
// Returns the best-effort parsed result. Never returns an error.
// For incomplete JSON, it closes open brackets/braces and retries.
func ParseStreamingJSON(partial string) map[string]any
```

Algorithm:
1. Try `json.Unmarshal` first (fast path for complete JSON).
2. If that fails, attempt to close the JSON:
   - Track open `{` and `[` characters (outside strings).
   - Track string state (inside/outside, handle `\"` escapes).
   - Trim trailing incomplete string values (find last `"`, truncate there).
   - Append closing `]` and `}` in reverse order of opens.
   - Retry `json.Unmarshal`.
3. If still fails, return empty `map[string]any{}`.

This replaces the `partial-json` npm package. The Go implementation is simpler because we only need object parsing (tool call arguments are always objects).

## 1.7 Prompt caching (`pkg/ai/cache.go`)

Prompt caching reduces cost and latency by letting providers cache portions of the request. Each provider implements caching differently, but gcode uses a unified `CacheRetention` setting.

### CacheRetention semantics

```go
type CacheRetention string

const (
    CacheNone  CacheRetention = "none"  // no caching
    CacheShort CacheRetention = "short" // ephemeral (default)
    CacheLong  CacheRetention = "long"  // extended TTL where supported
)
```

Default is `"short"`. Can be overridden via `StreamOptions.CacheRetention` or the `GCODE_CACHE_RETENTION` env var.

### Anthropic cache control

Anthropic supports explicit cache breakpoints via `cache_control` on content blocks.

```go
func resolveCacheRetention(opts *StreamOptions) CacheRetention {
    if opts.CacheRetention != "" {
        return CacheRetention(opts.CacheRetention)
    }
    if env := os.Getenv("GCODE_CACHE_RETENTION"); env == "long" {
        return CacheLong
    }
    return CacheShort
}

func getCacheControl(baseURL string, retention CacheRetention) map[string]any {
    if retention == CacheNone {
        return nil
    }
    cc := map[string]any{"type": "ephemeral"}
    // Extended TTL only on api.anthropic.com with "long" retention
    if retention == CacheLong && strings.Contains(baseURL, "api.anthropic.com") {
        cc["ttl"] = "1h"
    }
    return cc
}
```

Cache breakpoints are placed on:
1. **System prompt**: the last text block of the system instruction gets `cache_control`
2. **Last user message**: the last content block of the last user message gets `cache_control`

This caches the system prompt (stable across turns) and the conversation history up to the latest message, so only new content needs processing.

### OpenAI cache behavior

OpenAI caches automatically (no explicit breakpoints needed). The provider reads cache hit/write stats from `prompt_tokens_details.cached_tokens` and `prompt_tokens_details.cache_write_tokens` in the response.

For OpenRouter with Anthropic models: add `cache_control` on the last user/assistant text block (same as direct Anthropic).

### Google Gemini cache behavior

Gemini has context caching via a separate `cachedContent` API. For initial implementation, skip explicit caching for Google. Track `cachedContentTokenCount` from usage if available.

### Usage normalization

All providers normalize cache stats into the unified `Usage` struct:

```go
// OpenAI: cached_tokens, cache_write_tokens
// Anthropic: cache_read_input_tokens, cache_creation_input_tokens  
// Google: cachedContentTokenCount
// All mapped to: Usage.CacheRead, Usage.CacheWrite
```

Some providers (observed on OpenRouter) report `cached_tokens` as the sum of previous cache hits plus current writes. Normalize by subtracting `cache_write_tokens` from `cached_tokens` to get true `CacheRead`.

```go
cacheReadTokens := reportedCachedTokens
if cacheWriteTokens > 0 {
    cacheReadTokens = max(0, reportedCachedTokens - cacheWriteTokens)
}
input := max(0, promptTokens - cacheReadTokens - cacheWriteTokens)
```

## 1.8 Cost calculation (`pkg/ai/cost.go`)

```go
func CalculateCost(model Model, usage Usage) Cost {
    return Cost{
        Input:      (model.Cost.Input / 1_000_000) * float64(usage.Input),
        Output:     (model.Cost.Output / 1_000_000) * float64(usage.Output),
        CacheRead:  (model.Cost.CacheRead / 1_000_000) * float64(usage.CacheRead),
        CacheWrite: (model.Cost.CacheWrite / 1_000_000) * float64(usage.CacheWrite),
        Total:      /* sum of above */,
    }
}
```

## 1.8 OpenAI Completions provider (`pkg/ai/providers/openai.go`)

### Wire format

Raw HTTP, no SDK. Build the request manually.

```go
func streamOpenAICompletions(model Model, ctx Context, opts *StreamOptions) *AssistantMessageEventStream {
    stream := NewAssistantMessageEventStream()
    go func() {
        defer func() {
            if r := recover(); r != nil {
                // push error event
            }
        }()

        // 1. Build request body
        body := buildOpenAIRequest(model, ctx, opts)

        // 2. HTTP POST to model.BaseURL + "/chat/completions"
        req, _ := http.NewRequestWithContext(opts.Signal, "POST", url, bytes.NewReader(body))
        req.Header.Set("Content-Type", "application/json")
        req.Header.Set("Authorization", "Bearer "+opts.APIKey)

        // 3. Send request
        resp, err := http.DefaultClient.Do(req)
        // handle errors -> push error event

        // 4. Parse SSE stream
        scanner := NewSSEScanner(resp.Body)
        for scanner.Scan() {
            event := scanner.Event()
            if event.Data == "[DONE]" {
                break
            }
            // parse JSON chunk, update partial message, push events
        }

        // 5. Push done event
    }()
    return stream
}
```

### SSE parser

```go
type SSEScanner struct {
    scanner *bufio.Scanner
    event   SSEEvent
}

type SSEEvent struct {
    Event string
    Data  string
}

func NewSSEScanner(r io.Reader) *SSEScanner
func (s *SSEScanner) Scan() bool
func (s *SSEScanner) Event() SSEEvent
```

SSE format: lines of `data: {...}\n\n`. Some providers use `event: ` prefixes. Handle both.

### Message conversion

```go
func convertMessagesToOpenAI(messages []Message, compat *OpenAICompat) []openAIMessage
```

Rules (from pi):
- System prompt: use `developer` role if `compat.SupportsDeveloperRole`, else `system`
- UserMessage with string content: `{"role":"user","content":"text"}`
- UserMessage with images: `{"role":"user","content":[{"type":"text",...},{"type":"image_url","url":"data:mime;base64,data"}]}`
- AssistantMessage: extract text into `content` string, tool calls into `tool_calls` array
  - Thinking blocks: if `compat.RequiresThinkingAsText`, wrap in `<thinking>...</thinking>` text prefix
  - Tool calls: `{"id":"...","type":"function","function":{"name":"...","arguments":"{}"}}`
- ToolResultMessage: `{"role":"tool","content":"text","tool_call_id":"..."}`
  - If `compat.RequiresToolResultName`: add `"name":"toolName"`
  - Images in tool results: emit as separate `user` message after the tool result

### Chunk processing state machine

Track `currentBlock` (text, thinking, or toolcall). On each SSE chunk:

```go
type chunkState struct {
    currentBlock Content        // active block being accumulated
    partialArgs  string          // tool call argument buffer
    output       *AssistantMessage // in-progress message
}
```

For each `choice.delta`:
- `delta.content != ""`: text block. If new, finishCurrentBlock + emit `text_start`. Emit `text_delta`.
- `delta.reasoning_content != ""` (or `reasoning`, `reasoning_text`): thinking block. Similar pattern.
- `delta.tool_calls`: for each tool call delta:
  - New tool call ID: finishCurrentBlock + create ToolCall + emit `toolcall_start`
  - Append `function.arguments` to `partialArgs`, parse with `ParseStreamingJSON`, emit `toolcall_delta`

### OpenAI compat flags (`pkg/ai/providers/compat.go`)

```go
type OpenAICompat struct {
    SupportsDeveloperRole           bool
    SupportsReasoningEffort         bool
    ReasoningEffortMap              map[ThinkingLevel]string
    SupportsUsageInStreaming         bool
    MaxTokensField                  string // "max_completion_tokens" or "max_tokens"
    RequiresToolResultName          bool
    RequiresAssistantAfterToolResult bool
    RequiresThinkingAsText          bool
    ThinkingFormat                  string // "openai"|"openrouter"|"zai"|"qwen"
    SupportsStrictMode              bool
}

func DetectCompat(model Model) OpenAICompat {
    // Auto-detect from model.Provider and model.BaseURL
    switch model.Provider {
    case ProviderOpenAI:
        return OpenAICompat{
            SupportsDeveloperRole: true,
            SupportsReasoningEffort: true,
            MaxTokensField: "max_completion_tokens",
            SupportsUsageInStreaming: true,
            SupportsStrictMode: true,
        }
    case ProviderGroq:
        return OpenAICompat{
            MaxTokensField: "max_tokens",
            RequiresToolResultName: true,
        }
    // ... etc
    }
}

func GetCompat(model Model) OpenAICompat {
    detected := DetectCompat(model)
    if model.Compat != nil {
        // Merge: model.Compat fields override detected
        mergeCompat(&detected, model.Compat)
    }
    return detected
}
```

## 1.9 Anthropic provider (`pkg/ai/providers/anthropic.go`)

Same pattern as OpenAI but different wire format.

### Wire format

POST to `https://api.anthropic.com/v1/messages` with:
- Header: `x-api-key`, `anthropic-version: 2023-06-01`, `content-type: application/json`
- Body: `{"model":"...","max_tokens":...,"system":"...","messages":[...],"stream":true}`

### Anthropic SSE events

Different from OpenAI. Anthropic uses named event types:

```
event: message_start
data: {"type":"message_start","message":{...}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":15}}

event: message_stop
data: {"type":"message_stop"}
```

### Block tracking

Anthropic uses `index` to identify content blocks. Multiple blocks can interleave (text, thinking, tool_use).

```go
type anthropicBlock struct {
    index      int
    content    Content
    partialJSON string // for tool_use blocks
}

blocks := make(map[int]*anthropicBlock)
```

On `content_block_start`: create new block entry.
On `content_block_delta`: find block by index, accumulate, emit delta event.
On `content_block_stop`: finalize block, emit end event.

### Thinking support

- Budget-based (older models): `"thinking":{"type":"enabled","budget_tokens":N}`
- Adaptive (newer models): `"thinking":{"type":"adaptive"}` + `"output_config":{"effort":"low"|"medium"|"high"|"max"}`

The `streamSimpleAnthropic` function maps `ThinkingLevel` to these parameters.

### Message conversion (Anthropic format)

```go
func convertMessagesToAnthropic(systemPrompt string, messages []Message) (string, []anthropicMessage)
```

Key differences from OpenAI:
- System prompt is a top-level field, not a message
- Images use `{"type":"image","source":{"type":"base64","media_type":"...","data":"..."}}`
- Tool calls are `{"type":"tool_use","id":"...","name":"...","input":{...}}`
- Tool results are `{"role":"user","content":[{"type":"tool_result","tool_use_id":"...","content":"..."}]}`
- Thinking blocks preserved with signatures when same model

## 1.10 Google Gemini provider (`pkg/ai/providers/google.go`)

Same pattern as OpenAI and Anthropic but with Gemini's wire format.

### Wire format

POST to `https://generativelanguage.googleapis.com/v1beta/models/{model}:streamGenerateContent` with:
- API key via query param `?key=<key>` (preferred) or `x-goog-api-key` header
- Header: `content-type: application/json`
- Body uses `contents` array with `parts`, not `messages`

```go
func streamGoogleGemini(model Model, ctx Context, opts *StreamOptions) *AssistantMessageEventStream {
    stream := NewAssistantMessageEventStream()
    go func() {
        defer func() {
            if r := recover(); r != nil {
                // push error event
            }
        }()

        // 1. Build request body
        body := buildGoogleRequest(model, ctx, opts)

        // 2. HTTP POST to model.BaseURL + "/models/" + model.ID + ":streamGenerateContent?key=" + opts.APIKey + "&alt=sse"
        req, _ := http.NewRequestWithContext(opts.Signal, "POST", url, bytes.NewReader(body))
        req.Header.Set("Content-Type", "application/json")

        // 3. Send request
        resp, err := http.DefaultClient.Do(req)
        // handle errors -> push error event

        // 4. Parse SSE stream
        scanner := NewSSEScanner(resp.Body)
        for scanner.Scan() {
            event := scanner.Event()
            // parse JSON chunk, update partial message, push events
        }

        // 5. Push done event
    }()
    return stream
}
```

### Request body structure

```json
{
  "contents": [
    {"role": "user", "parts": [{"text": "Hello"}]},
    {"role": "model", "parts": [{"text": "Hi there"}]}
  ],
  "system_instruction": {
    "parts": [{"text": "You are a helpful assistant."}]
  },
  "tools": [{
    "function_declarations": [{
      "name": "read_file",
      "description": "Read a file",
      "parameters": { /* JSON Schema */ }
    }]
  }],
  "generation_config": {
    "temperature": 0.7,
    "max_output_tokens": 8192
  }
}
```

Key differences from OpenAI/Anthropic:
- Roles are `user` and `model` (not `assistant`)
- System prompt is a top-level `system_instruction` field with `parts`
- Content is `parts` array, not `content`
- Tool definitions live under `tools[].function_declarations`

### Message conversion (Gemini format)

```go
func convertMessagesToGoogle(systemPrompt string, messages []Message) (googleSystemInstruction, []googleContent)
```

Rules:
- System prompt becomes `system_instruction.parts[{text: "..."}]`
- UserMessage text: `{"role": "user", "parts": [{"text": "..."}]}`
- UserMessage with images: `{"role": "user", "parts": [{"text": "..."}, {"inline_data": {"mime_type": "...", "data": "base64"}}]}`
- AssistantMessage text: `{"role": "model", "parts": [{"text": "..."}]}`
- AssistantMessage tool calls: `{"role": "model", "parts": [{"functionCall": {"name": "...", "args": {...}}}]}`
- ToolResultMessage: `{"role": "user", "parts": [{"functionResponse": {"name": "...", "response": {"result": "..."}}}]}`
- Thinking blocks: convert to plain text (Gemini has its own thinking mechanism via `thinkingConfig`)

### Tool calls

Gemini uses `functionCall` and `functionResponse` instead of OpenAI's `tool_calls`/`tool` pattern:

- In assistant response: `{"functionCall": {"name": "tool_name", "args": {"key": "value"}}}`
- In tool result: `{"functionResponse": {"name": "tool_name", "response": {"result": "..."}}}`

Tool call IDs are not used by Gemini. Generate synthetic IDs for internal tracking to match the `ToolCall` type.

### SSE response format

With `alt=sse` query param, Gemini streams SSE events:

```
data: {"candidates":[{"content":{"parts":[{"text":"Hello"}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5}}
```

Each SSE chunk contains a full `candidates` array. Extract `candidates[0].content.parts` and diff against previous state to emit delta events.

### Chunk processing

```go
type googleChunkState struct {
    lastTextLen   int              // track text accumulation for delta computation
    partialArgs   string           // tool call argument buffer
    output        *AssistantMessage
}
```

For each SSE chunk:
- `parts[].text`: compare length against `lastTextLen`, emit delta for new characters
- `parts[].functionCall`: emit `toolcall_start` + `toolcall_end` (Gemini sends complete function calls, not streamed)
- `finishReason: "STOP"`: emit done event
- `finishReason: "MAX_TOKENS"`: emit done with `StopReasonLength`

### Registration

```go
func init() {
    RegisterProvider(&ApiProvider{
        Api:          ApiGoogleGemini,
        Stream:       streamGoogleGemini,
        StreamSimple: streamSimpleGoogleGemini,
    })
}
```

## 1.11 Message transformation (`pkg/ai/transform.go`)

Cross-provider message normalization. Called before sending to any provider.

```go
func TransformMessages(messages []Message, targetModel Model) []Message
```

Rules (from pi's `transform-messages.ts`):
1. Build tool call ID map for normalization
2. Handle thinking blocks:
   - Same model with signature: keep as-is
   - Cross-model: convert thinking to plain text (lose signature)
   - Redacted thinking from different model: drop entirely
   - Empty thinking: drop
3. Strip `thoughtSignature` on ToolCall for cross-model
4. Patch orphaned tool calls: if assistant has tool calls but no matching tool results follow before the next user/assistant message, insert synthetic error tool results
5. Skip errored/aborted assistant messages entirely

## 1.12 Faux provider (`pkg/ai/providers/faux.go`)

In-memory provider for testing. Returns canned responses.

```go
type FauxResponse struct {
    Text      string
    ToolCalls []ToolCall
    Thinking  string
    Delay     time.Duration // per-token delay for simulating streaming
}

type FauxProvider struct {
    Responses []FauxResponse
    callIndex int
}

func (f *FauxProvider) Stream(model Model, ctx Context, opts *StreamOptions) *AssistantMessageEventStream
```

Emits events character-by-character with configurable delay. Cycles through `Responses` on successive calls.

## 1.13 Model data (`pkg/ai/models.go`)

Hardcoded model definitions for initial implementation. Later, generate from a JSON file.

```go
var Models = map[Provider][]Model{
    ProviderAnthropic: {
        {ID: "claude-sonnet-4-20250514", Name: "Claude Sonnet 4", Api: ApiAnthropicMessages, ...},
        {ID: "claude-haiku-4-20250514", Name: "Claude Haiku 4", Api: ApiAnthropicMessages, ...},
    },
    ProviderOpenAI: {
        {ID: "gpt-4.1", Name: "GPT-4.1", Api: ApiOpenAICompletions, ...},
        {ID: "o3-mini", Name: "o3-mini", Api: ApiOpenAICompletions, ...},
    },
    ProviderGoogle: {
        {ID: "gemini-2.5-pro", Name: "Gemini 2.5 Pro", Api: ApiGoogleGemini, Provider: ProviderGoogle, BaseURL: "https://generativelanguage.googleapis.com/v1beta", Reasoning: true, ...},
        {ID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash", Api: ApiGoogleGemini, Provider: ProviderGoogle, BaseURL: "https://generativelanguage.googleapis.com/v1beta", Reasoning: true, ...},
    },
}

func GetModel(provider Provider, id string) (Model, bool)
func GetModels(provider Provider) []Model
func GetProviders() []Provider
```

## 1.14 Tests

### Unit tests

- `event_stream_test.go`: Push/pull semantics, concurrent access, end() idempotency, Result() blocking
- `json_parse_test.go`: Complete JSON, partial objects, partial strings, nested objects, empty input
- `cost_test.go`: Cost calculation with known values
- `schema_test.go`: Schema generation from structs with optional fields
- `transform_test.go`: Orphaned tool call patching, thinking block cross-model handling, error message skipping

### Integration tests (behind build tag)

```go
//go:build integration

func TestOpenAIStream(t *testing.T) {
    apiKey := os.Getenv("OPENAI_API_KEY")
    if apiKey == "" { t.Skip("OPENAI_API_KEY not set") }
    // Stream a simple prompt, verify events arrive in correct order
}

func TestAnthropicStream(t *testing.T) {
    apiKey := os.Getenv("ANTHROPIC_API_KEY")
    if apiKey == "" { t.Skip("ANTHROPIC_API_KEY not set") }
    // Same
}

func TestGoogleGeminiStream(t *testing.T) {
    apiKey := os.Getenv("GOOGLE_API_KEY")
    if apiKey == "" { t.Skip("GOOGLE_API_KEY not set") }
    // Stream a simple prompt, verify events arrive in correct order
    // Verify functionCall tool calls are emitted correctly
}
```

### Verification criteria

- [ ] `go test ./pkg/ai/...` passes
- [ ] Faux provider streams text, tool calls, and thinking blocks
- [ ] SSE parser handles chunked responses, empty lines, `[DONE]` sentinel
- [ ] Partial JSON parser handles: `{"path":"/foo"`, `{"path":"/foo","off`, `{"edits":[{"old`
- [ ] Error events are emitted for network errors, invalid API keys, malformed responses
- [ ] Context cancellation (via `context.Context`) stops the HTTP request and emits an aborted event
- [ ] Google Gemini provider streams text and tool calls
- [ ] Google Gemini message conversion handles `contents`/`parts` format, `system_instruction`, `functionCall`/`functionResponse`
- [ ] Cost calculation matches expected values for known models
