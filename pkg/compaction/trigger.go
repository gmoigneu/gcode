package compaction

import (
	"regexp"
	"strings"

	"github.com/gmoigneu/gcode/pkg/ai"
)

// ShouldCompact returns true when the accumulated context tokens have
// exceeded the safe threshold (contextWindow - reserveTokens). Returns
// false when compaction is disabled or when the context window is 0
// (unknown).
func ShouldCompact(contextTokens, contextWindow int, settings CompactionSettings) bool {
	if !settings.Enabled || contextWindow <= 0 {
		return false
	}
	threshold := contextWindow - settings.ReserveTokens
	if threshold <= 0 {
		return false
	}
	return contextTokens > threshold
}

// overflowPatterns are provider-specific error fragments that indicate a
// context-window overflow. Matched case-insensitively against the error
// message.
var overflowPatterns = []*regexp.Regexp{
	regexp.MustCompile(`prompt is too long`),
	regexp.MustCompile(`maximum context length`),
	regexp.MustCompile(`maximum number of tokens`),
	regexp.MustCompile(`exceeds the maximum`),
	regexp.MustCompile(`too many tokens`),
	regexp.MustCompile(`context_length_exceeded`),
	regexp.MustCompile(`content_too_large`),
	regexp.MustCompile(`this model's maximum context length`),
	regexp.MustCompile(`resource_exhausted`),
	regexp.MustCompile(`token limit`),
	regexp.MustCompile(`context window`),
	regexp.MustCompile(`input too long`),
	regexp.MustCompile(`request too large`),
}

// nonOverflowPatterns are error fragments that look like overflow at a
// glance but are actually unrelated (rate limiting, billing, etc).
var nonOverflowPatterns = []*regexp.Regexp{
	regexp.MustCompile(`rate limit`),
	regexp.MustCompile(`rate_limit`),
	regexp.MustCompile(`throttl`),
	regexp.MustCompile(`too many requests`),
	regexp.MustCompile(`\b429\b`),
	regexp.MustCompile(`quota`),
	regexp.MustCompile(`billing`),
}

// IsContextOverflow reports whether an error message indicates that the
// context window was exceeded. Handles two signals:
//  1. "Silent" overflow — usage.Input > contextWindow despite no error text
//  2. Pattern-matched error messages (with rate-limit exclusions applied first)
func IsContextOverflow(errMsg string, usage *ai.Usage, contextWindow int) bool {
	if usage != nil && contextWindow > 0 && usage.Input > contextWindow {
		return true
	}
	if errMsg == "" {
		return false
	}
	lower := strings.ToLower(errMsg)

	for _, p := range nonOverflowPatterns {
		if p.MatchString(lower) {
			return false
		}
	}
	for _, p := range overflowPatterns {
		if p.MatchString(lower) {
			return true
		}
	}
	return false
}
