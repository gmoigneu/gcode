package ai

import (
	"os"
	"strings"
)

// ResolveCacheRetention returns the effective CacheRetention for a request.
// Precedence:
//  1. opts.CacheRetention (if a known value)
//  2. GCODE_CACHE_RETENTION env var (if a known value)
//  3. CacheShort (default)
func ResolveCacheRetention(opts *StreamOptions) CacheRetention {
	if opts != nil {
		if v := normalizeCacheRetention(opts.CacheRetention); v != "" {
			return v
		}
	}
	if v := normalizeCacheRetention(os.Getenv("GCODE_CACHE_RETENTION")); v != "" {
		return v
	}
	return CacheShort
}

func normalizeCacheRetention(s string) CacheRetention {
	switch CacheRetention(s) {
	case CacheNone, CacheShort, CacheLong:
		return CacheRetention(s)
	}
	return ""
}

// GetCacheControl returns the Anthropic cache_control object for a given
// base URL and retention level. Returns nil for CacheNone. Extended TTL (1h)
// is only applied to direct Anthropic endpoints; third-party proxies get a
// plain ephemeral breakpoint.
func GetCacheControl(baseURL string, retention CacheRetention) map[string]any {
	if retention == CacheNone {
		return nil
	}
	cc := map[string]any{"type": "ephemeral"}
	if retention == CacheLong && strings.Contains(baseURL, "api.anthropic.com") {
		cc["ttl"] = "1h"
	}
	return cc
}

// CacheBreakpoint marks a single location in the request where a provider
// should attach a cache_control object. MessageIndex == -1 refers to the
// system prompt; otherwise it is an index into Context.Messages.
type CacheBreakpoint struct {
	// MessageIndex is the index in Context.Messages, or -1 for the system prompt.
	MessageIndex int
	// BlockIndex is the index of the content block within the message. For
	// the system prompt, 0 refers to the last text fragment.
	BlockIndex int
}

// PlaceAnthropicCacheBreakpoints returns the breakpoints a provider should
// install for Anthropic-style cache_control placement. Two positions are
// produced when available:
//  1. The system prompt's last block (MessageIndex = -1)
//  2. The last UserMessage's last content block
//
// CacheNone yields no breakpoints. Providers call this to place cache hints
// consistently with the rest of gcode.
func PlaceAnthropicCacheBreakpoints(messages []Message, systemBlockCount int, retention CacheRetention, baseURL string) []CacheBreakpoint {
	if retention == CacheNone {
		return nil
	}
	var out []CacheBreakpoint
	if systemBlockCount > 0 {
		out = append(out, CacheBreakpoint{MessageIndex: -1, BlockIndex: systemBlockCount - 1})
	}
	if bp, ok := lastUserBreakpoint(messages); ok {
		out = append(out, bp)
	}
	return out
}

func lastUserBreakpoint(messages []Message) (CacheBreakpoint, bool) {
	for i := len(messages) - 1; i >= 0; i-- {
		u, ok := messages[i].(*UserMessage)
		if !ok {
			continue
		}
		if len(u.Content) == 0 {
			return CacheBreakpoint{}, false
		}
		return CacheBreakpoint{MessageIndex: i, BlockIndex: len(u.Content) - 1}, true
	}
	return CacheBreakpoint{}, false
}

// NormalizeCacheUsage converts provider-reported token counts into the
// canonical (input, cacheRead, cacheWrite) triple. It is intended for
// OpenAI-compatible providers where cachedTokens may include the current
// write (as observed on OpenRouter).
//
// NormalizeCacheUsage subtracts cacheWriteTokens from cachedTokens to
// recover the true read count, then removes both from promptTokens to get
// the uncached "input" count. When the provider reports writes as zero,
// cachedTokens is treated as a pure read.
//
// Providers that already report cache read and write as disjoint counters
// (e.g. Anthropic) should populate Usage directly without going through
// this helper.
func NormalizeCacheUsage(promptTokens, cachedTokens, cacheWriteTokens int) (input, cacheRead, cacheWrite int) {
	cacheWrite = maxInt(cacheWriteTokens, 0)
	cacheRead = maxInt(cachedTokens-cacheWrite, 0)
	input = maxInt(promptTokens-cacheRead-cacheWrite, 0)
	return
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
