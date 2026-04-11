package compaction

import (
	"testing"

	"github.com/gmoigneu/gcode/pkg/ai"
)

// ----- ShouldCompact -----

func TestShouldCompactUnderThreshold(t *testing.T) {
	// window = 100, reserve = 10 → threshold = 90. 50 < 90 → no compact.
	if ShouldCompact(50, 100, CompactionSettings{Enabled: true, ReserveTokens: 10}) {
		t.Error("should not compact under threshold")
	}
}

func TestShouldCompactAtThreshold(t *testing.T) {
	// Exactly at threshold — ShouldCompact returns false ("strictly greater").
	if ShouldCompact(90, 100, CompactionSettings{Enabled: true, ReserveTokens: 10}) {
		t.Error("should not compact exactly at threshold")
	}
}

func TestShouldCompactOverThreshold(t *testing.T) {
	if !ShouldCompact(95, 100, CompactionSettings{Enabled: true, ReserveTokens: 10}) {
		t.Error("should compact over threshold")
	}
}

func TestShouldCompactDisabled(t *testing.T) {
	if ShouldCompact(1_000_000, 100, CompactionSettings{Enabled: false, ReserveTokens: 10}) {
		t.Error("disabled settings should never compact")
	}
}

func TestShouldCompactUnknownWindow(t *testing.T) {
	if ShouldCompact(1_000_000, 0, CompactionSettings{Enabled: true, ReserveTokens: 10}) {
		t.Error("unknown window should not compact")
	}
}

func TestShouldCompactZeroThreshold(t *testing.T) {
	// reserve == window → threshold is zero → never compact.
	if ShouldCompact(1, 10, CompactionSettings{Enabled: true, ReserveTokens: 10}) {
		t.Error("zero threshold should not compact")
	}
}

// ----- IsContextOverflow -----

func TestOverflowAnthropicPatterns(t *testing.T) {
	msgs := []string{
		"prompt is too long",
		"exceeds the maximum context length",
		"content_too_large",
	}
	for _, m := range msgs {
		if !IsContextOverflow(m, nil, 0) {
			t.Errorf("expected overflow for %q", m)
		}
	}
}

func TestOverflowOpenAIPatterns(t *testing.T) {
	msgs := []string{
		"This model's maximum context length is 128000 tokens",
		"context_length_exceeded",
		"request too large",
	}
	for _, m := range msgs {
		if !IsContextOverflow(m, nil, 0) {
			t.Errorf("expected overflow for %q", m)
		}
	}
}

func TestOverflowGooglePatterns(t *testing.T) {
	msgs := []string{
		"RESOURCE_EXHAUSTED: too many tokens",
		"The request exceeds the maximum number of tokens allowed",
	}
	for _, m := range msgs {
		if !IsContextOverflow(m, nil, 0) {
			t.Errorf("expected overflow for %q", m)
		}
	}
}

func TestOverflowExcludesRateLimit(t *testing.T) {
	msgs := []string{
		"rate limit exceeded",
		"429 Too Many Requests",
		"quota exhausted",
		"billing not configured",
		"throttled",
	}
	for _, m := range msgs {
		if IsContextOverflow(m, nil, 0) {
			t.Errorf("should not match as overflow: %q", m)
		}
	}
}

func TestOverflowSilentInputExceedsWindow(t *testing.T) {
	usage := &ai.Usage{Input: 200000}
	if !IsContextOverflow("", usage, 100000) {
		t.Error("silent overflow not detected")
	}
}

func TestOverflowEmptyString(t *testing.T) {
	if IsContextOverflow("", nil, 0) {
		t.Error("empty error should not overflow without usage signal")
	}
}

func TestOverflowNonMatchingError(t *testing.T) {
	if IsContextOverflow("connection refused", nil, 0) {
		t.Error("unrelated error should not match")
	}
}

func TestOverflowCaseInsensitive(t *testing.T) {
	if !IsContextOverflow("PROMPT IS TOO LONG", nil, 0) {
		t.Error("should be case-insensitive")
	}
}

// ----- settings constants -----

func TestDefaultCompactionSettings(t *testing.T) {
	if !DefaultCompactionSettings.Enabled {
		t.Error("default should be enabled")
	}
	if DefaultCompactionSettings.ReserveTokens <= 0 {
		t.Errorf("default ReserveTokens = %d", DefaultCompactionSettings.ReserveTokens)
	}
	if DefaultCompactionSettings.KeepRecentTokens <= 0 {
		t.Errorf("default KeepRecentTokens = %d", DefaultCompactionSettings.KeepRecentTokens)
	}
}

func TestCompactionReasonConstants(t *testing.T) {
	if CompactionReasonThreshold != "threshold" {
		t.Errorf("threshold = %q", CompactionReasonThreshold)
	}
	if CompactionReasonOverflow != "overflow" {
		t.Errorf("overflow = %q", CompactionReasonOverflow)
	}
}
