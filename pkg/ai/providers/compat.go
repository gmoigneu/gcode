package providers

import "github.com/gmoigneu/gcode/pkg/ai"

// DetectCompat returns the default OpenAI-compat flags for a provider. The
// result is a fresh value the caller may mutate.
func DetectCompat(model ai.Model) ai.OpenAICompat {
	// Sensible defaults for unknown providers: chat/completions speakers
	// that use max_tokens and nothing fancy.
	c := ai.OpenAICompat{MaxTokensField: "max_tokens"}

	switch model.Provider {
	case ai.ProviderOpenAI:
		c.SupportsDeveloperRole = true
		c.SupportsReasoningEffort = true
		c.MaxTokensField = "max_completion_tokens"
		c.SupportsUsageInStreaming = true
		c.SupportsStrictMode = true
	case ai.ProviderGroq:
		c.RequiresToolResultName = true
	case ai.ProviderXAI:
		c.SupportsReasoningEffort = true
	case ai.ProviderCerebras:
		// Plain chat/completions with minimal features.
	case ai.ProviderOpenRouter:
		c.SupportsUsageInStreaming = true
	}
	return c
}

// GetCompat returns the effective compat flags for a model, overlaying any
// fields set in model.Compat onto the auto-detected defaults.
func GetCompat(model ai.Model) ai.OpenAICompat {
	detected := DetectCompat(model)
	if model.Compat != nil {
		mergeCompat(&detected, model.Compat)
	}
	return detected
}

// mergeCompat copies non-zero fields from override into dst.
func mergeCompat(dst, override *ai.OpenAICompat) {
	if override.SupportsDeveloperRole {
		dst.SupportsDeveloperRole = true
	}
	if override.SupportsReasoningEffort {
		dst.SupportsReasoningEffort = true
	}
	if len(override.ReasoningEffortMap) > 0 {
		dst.ReasoningEffortMap = override.ReasoningEffortMap
	}
	if override.SupportsUsageInStreaming {
		dst.SupportsUsageInStreaming = true
	}
	if override.MaxTokensField != "" {
		dst.MaxTokensField = override.MaxTokensField
	}
	if override.RequiresToolResultName {
		dst.RequiresToolResultName = true
	}
	if override.RequiresThinkingAsText {
		dst.RequiresThinkingAsText = true
	}
	if override.ThinkingFormat != "" {
		dst.ThinkingFormat = override.ThinkingFormat
	}
	if override.SupportsStrictMode {
		dst.SupportsStrictMode = true
	}
}
