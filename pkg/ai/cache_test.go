package ai

import (
	"testing"
)

func TestResolveCacheRetentionDefault(t *testing.T) {
	t.Setenv("GCODE_CACHE_RETENTION", "")
	got := ResolveCacheRetention(&StreamOptions{})
	if got != CacheShort {
		t.Errorf("default = %q, want %q", got, CacheShort)
	}
}

func TestResolveCacheRetentionFromOpts(t *testing.T) {
	t.Setenv("GCODE_CACHE_RETENTION", "short")
	got := ResolveCacheRetention(&StreamOptions{CacheRetention: "long"})
	if got != CacheLong {
		t.Errorf("opts override = %q, want %q", got, CacheLong)
	}

	got = ResolveCacheRetention(&StreamOptions{CacheRetention: "none"})
	if got != CacheNone {
		t.Errorf("opts none = %q", got)
	}
}

func TestResolveCacheRetentionFromEnv(t *testing.T) {
	t.Setenv("GCODE_CACHE_RETENTION", "long")
	got := ResolveCacheRetention(&StreamOptions{})
	if got != CacheLong {
		t.Errorf("env = %q, want long", got)
	}

	t.Setenv("GCODE_CACHE_RETENTION", "none")
	got = ResolveCacheRetention(&StreamOptions{})
	if got != CacheNone {
		t.Errorf("env none = %q", got)
	}
}

func TestResolveCacheRetentionNilOpts(t *testing.T) {
	t.Setenv("GCODE_CACHE_RETENTION", "")
	got := ResolveCacheRetention(nil)
	if got != CacheShort {
		t.Errorf("nil opts = %q, want short", got)
	}
}

func TestResolveCacheRetentionUnknownFallsBack(t *testing.T) {
	t.Setenv("GCODE_CACHE_RETENTION", "weird")
	got := ResolveCacheRetention(&StreamOptions{CacheRetention: "also-weird"})
	if got != CacheShort {
		t.Errorf("unknown = %q, want fallback short", got)
	}
}

func TestGetCacheControlNone(t *testing.T) {
	cc := GetCacheControl("https://api.anthropic.com/v1", CacheNone)
	if cc != nil {
		t.Errorf("CacheNone should return nil, got %v", cc)
	}
}

func TestGetCacheControlShort(t *testing.T) {
	cc := GetCacheControl("https://api.anthropic.com/v1", CacheShort)
	if cc["type"] != "ephemeral" {
		t.Errorf("type = %v, want ephemeral", cc["type"])
	}
	if _, ok := cc["ttl"]; ok {
		t.Error("short retention should not set ttl")
	}
}

func TestGetCacheControlLongAnthropic(t *testing.T) {
	cc := GetCacheControl("https://api.anthropic.com/v1/messages", CacheLong)
	if cc["type"] != "ephemeral" || cc["ttl"] != "1h" {
		t.Errorf("got %v, want ephemeral + 1h ttl", cc)
	}
}

func TestGetCacheControlLongNonAnthropic(t *testing.T) {
	// OpenRouter proxying Anthropic models: no extended TTL, just ephemeral.
	cc := GetCacheControl("https://openrouter.ai/api/v1", CacheLong)
	if cc["type"] != "ephemeral" {
		t.Errorf("type = %v", cc["type"])
	}
	if _, ok := cc["ttl"]; ok {
		t.Errorf("non-anthropic base should not have ttl, got %v", cc)
	}
}

func TestNormalizeCacheUsageNoWrite(t *testing.T) {
	// With zero writes, cachedTokens is pure read; input = prompt - read.
	input, read, write := NormalizeCacheUsage(1000, 200, 0)
	if input != 800 || read != 200 || write != 0 {
		t.Errorf("input=%d read=%d write=%d", input, read, write)
	}
}

func TestNormalizeCacheUsageOpenRouterCombined(t *testing.T) {
	// Some providers report cached_tokens as the SUM of previous reads + current writes.
	// With promptTokens=1000, cachedTokens=250 (includes 50 new writes), cacheWrites=50:
	// true cacheRead = 250 - 50 = 200
	// input = 1000 - 200 - 50 = 750
	input, read, write := NormalizeCacheUsage(1000, 250, 50)
	if input != 750 || read != 200 || write != 50 {
		t.Errorf("input=%d read=%d write=%d", input, read, write)
	}
}

func TestNormalizeCacheUsageNegativeClamps(t *testing.T) {
	// Defensive: if cachedTokens < cacheWrites, clamp cacheRead to 0.
	input, read, write := NormalizeCacheUsage(1000, 10, 50)
	if read != 0 {
		t.Errorf("read = %d, want 0", read)
	}
	if input < 0 {
		t.Errorf("input = %d, should not go negative", input)
	}
	_ = write
}

func TestPlaceAnthropicCacheBreakpointsEmpty(t *testing.T) {
	// With CacheNone, nothing should be touched.
	got := PlaceAnthropicCacheBreakpoints(nil, 0, CacheNone, "https://api.anthropic.com")
	if got != nil {
		t.Errorf("CacheNone should return nil breakpoints, got %v", got)
	}
}

func TestPlaceAnthropicCacheBreakpointsSystemAndLastUser(t *testing.T) {
	msgs := []Message{
		&UserMessage{Content: []Content{&TextContent{Text: "first"}}},
		&AssistantMessage{Content: []Content{&TextContent{Text: "reply"}}},
		&UserMessage{Content: []Content{
			&TextContent{Text: "second"},
			&TextContent{Text: "third"},
		}},
	}
	bps := PlaceAnthropicCacheBreakpoints(msgs, 1, CacheShort, "https://api.anthropic.com")
	// Two breakpoints expected: one for system prompt, one for last user message
	// last content block.
	if len(bps) != 2 {
		t.Fatalf("got %d breakpoints, want 2", len(bps))
	}
	// system breakpoint
	if bps[0].MessageIndex != -1 || bps[0].BlockIndex != 0 {
		t.Errorf("system bp = %+v", bps[0])
	}
	// last user message is messages[2], last content block is index 1
	if bps[1].MessageIndex != 2 || bps[1].BlockIndex != 1 {
		t.Errorf("user bp = %+v", bps[1])
	}
}

func TestPlaceAnthropicCacheBreakpointsNoSystem(t *testing.T) {
	msgs := []Message{
		&UserMessage{Content: []Content{&TextContent{Text: "only"}}},
	}
	bps := PlaceAnthropicCacheBreakpoints(msgs, 0, CacheShort, "https://api.anthropic.com")
	if len(bps) != 1 {
		t.Fatalf("got %d breakpoints, want 1 (no system)", len(bps))
	}
	if bps[0].MessageIndex != 0 {
		t.Errorf("bp = %+v", bps[0])
	}
}

func TestPlaceAnthropicCacheBreakpointsNoUser(t *testing.T) {
	// If the last message is not a UserMessage, still no user breakpoint.
	msgs := []Message{
		&AssistantMessage{Content: []Content{&TextContent{Text: "reply"}}},
	}
	bps := PlaceAnthropicCacheBreakpoints(msgs, 1, CacheShort, "https://api.anthropic.com")
	// Only the system bp.
	if len(bps) != 1 || bps[0].MessageIndex != -1 {
		t.Errorf("got %+v", bps)
	}
}
