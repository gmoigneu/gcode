package providers

import (
	"testing"

	"github.com/gmoigneu/gcode/pkg/ai"
)

func TestDetectCompatOpenAI(t *testing.T) {
	c := DetectCompat(ai.Model{Provider: ai.ProviderOpenAI})
	if !c.SupportsDeveloperRole {
		t.Error("OpenAI should support developer role")
	}
	if !c.SupportsReasoningEffort {
		t.Error("OpenAI should support reasoning_effort")
	}
	if c.MaxTokensField != "max_completion_tokens" {
		t.Errorf("MaxTokensField = %q", c.MaxTokensField)
	}
	if !c.SupportsUsageInStreaming {
		t.Error("OpenAI should support usage in streaming")
	}
}

func TestDetectCompatGroq(t *testing.T) {
	c := DetectCompat(ai.Model{Provider: ai.ProviderGroq})
	if c.MaxTokensField != "max_tokens" {
		t.Errorf("MaxTokensField = %q", c.MaxTokensField)
	}
	if !c.RequiresToolResultName {
		t.Error("Groq should require tool result name")
	}
	if c.SupportsDeveloperRole {
		t.Error("Groq should not support developer role")
	}
}

func TestDetectCompatXAI(t *testing.T) {
	c := DetectCompat(ai.Model{Provider: ai.ProviderXAI})
	if c.MaxTokensField != "max_tokens" {
		t.Errorf("MaxTokensField = %q", c.MaxTokensField)
	}
}

func TestDetectCompatOpenRouter(t *testing.T) {
	c := DetectCompat(ai.Model{Provider: ai.ProviderOpenRouter})
	if !c.SupportsUsageInStreaming {
		t.Error("OpenRouter should include usage in streaming")
	}
	if c.MaxTokensField != "max_tokens" {
		t.Errorf("MaxTokensField = %q", c.MaxTokensField)
	}
}

func TestDetectCompatUnknownProvider(t *testing.T) {
	c := DetectCompat(ai.Model{Provider: "unknown"})
	if c.MaxTokensField != "max_tokens" {
		t.Errorf("default MaxTokensField = %q", c.MaxTokensField)
	}
}

func TestGetCompatWithoutOverride(t *testing.T) {
	m := ai.Model{Provider: ai.ProviderOpenAI}
	c := GetCompat(m)
	if !c.SupportsDeveloperRole {
		t.Error("expected detected defaults to apply")
	}
}

func TestGetCompatOverrideMaxTokensField(t *testing.T) {
	m := ai.Model{
		Provider: ai.ProviderOpenAI,
		Compat: &ai.OpenAICompat{
			MaxTokensField: "max_tokens",
		},
	}
	c := GetCompat(m)
	if c.MaxTokensField != "max_tokens" {
		t.Errorf("override not applied: %q", c.MaxTokensField)
	}
	// Other fields preserved from detection.
	if !c.SupportsDeveloperRole {
		t.Error("non-overridden fields should be preserved")
	}
}

func TestGetCompatOverrideBoolTrue(t *testing.T) {
	m := ai.Model{
		Provider: ai.ProviderGroq,
		Compat: &ai.OpenAICompat{
			SupportsDeveloperRole: true,
		},
	}
	c := GetCompat(m)
	if !c.SupportsDeveloperRole {
		t.Error("override of bool=true should apply")
	}
}

func TestGetCompatOverrideReasoningEffortMap(t *testing.T) {
	m := ai.Model{
		Provider: ai.ProviderOpenAI,
		Compat: &ai.OpenAICompat{
			ReasoningEffortMap: map[ai.ThinkingLevel]string{
				ai.ThinkingHigh: "very-high",
			},
		},
	}
	c := GetCompat(m)
	if c.ReasoningEffortMap[ai.ThinkingHigh] != "very-high" {
		t.Errorf("map override not applied: %v", c.ReasoningEffortMap)
	}
}
