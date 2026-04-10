package ai

import (
	"path/filepath"
	"sort"
	"sync"
	"testing"
)

func TestDefaultsForProviderCoversKnown(t *testing.T) {
	cases := []Provider{
		ProviderAnthropic, ProviderOpenAI, ProviderGoogle,
		ProviderGroq, ProviderXAI, ProviderCerebras, ProviderOpenRouter,
	}
	for _, p := range cases {
		api, baseURL := defaultsForProvider(p)
		if api == "" {
			t.Errorf("%s: empty api", p)
		}
		if baseURL == "" {
			t.Errorf("%s: empty baseURL", p)
		}
	}
}

func TestDefaultsForProviderAnthropic(t *testing.T) {
	api, baseURL := defaultsForProvider(ProviderAnthropic)
	if api != ApiAnthropicMessages {
		t.Errorf("api = %q", api)
	}
	if baseURL == "" {
		t.Error("empty base url")
	}
}

func TestModelDataToModel(t *testing.T) {
	md := ModelData{
		ID:            "claude-opus-4-6",
		Name:          "Claude Opus 4.6",
		Provider:      "anthropic",
		ContextWindow: 200_000,
		MaxTokens:     32_000,
		Cost:          ModelCost{Input: 15, Output: 75},
	}
	m := modelDataToModel(md)
	if m.ID != "claude-opus-4-6" || m.Name != "Claude Opus 4.6" {
		t.Errorf("id/name = %q/%q", m.ID, m.Name)
	}
	if m.Provider != ProviderAnthropic {
		t.Errorf("provider = %q", m.Provider)
	}
	if m.Api != ApiAnthropicMessages {
		t.Errorf("api = %q", m.Api)
	}
	if m.BaseURL == "" {
		t.Error("base url empty")
	}
	if m.Cost.Input != 15 || m.Cost.Output != 75 {
		t.Errorf("cost = %+v", m.Cost)
	}
	if m.ContextWindow != 200_000 || m.MaxTokens != 32_000 {
		t.Errorf("window/max = %d/%d", m.ContextWindow, m.MaxTokens)
	}
}

func TestGetModelFromHardcodedFallback(t *testing.T) {
	withIsolatedModelRegistry(t)

	// Hardcoded cache contains claude-opus-4-6 as a safe minimum.
	m, ok := GetModel(ProviderAnthropic, "claude-opus-4-6")
	if !ok {
		t.Fatal("claude-opus-4-6 should be in the hardcoded fallback")
	}
	if m.Api != ApiAnthropicMessages {
		t.Errorf("api = %q", m.Api)
	}
	if m.Cost.Input <= 0 || m.Cost.Output <= 0 {
		t.Errorf("hardcoded model missing cost: %+v", m.Cost)
	}
}

func TestGetModelMissingReturnsFalse(t *testing.T) {
	withIsolatedModelRegistry(t)

	if _, ok := GetModel(ProviderAnthropic, "definitely-not-a-model"); ok {
		t.Error("missing model should return !ok")
	}
}

func TestGetModelsReturnsAllForProvider(t *testing.T) {
	withIsolatedModelRegistry(t)

	list := GetModels(ProviderAnthropic)
	if len(list) == 0 {
		t.Fatal("expected at least one anthropic model")
	}
	sawOpus := false
	for _, m := range list {
		if m.ID == "claude-opus-4-6" {
			sawOpus = true
		}
		if m.Provider != ProviderAnthropic {
			t.Errorf("provider mismatch: %q", m.Provider)
		}
	}
	if !sawOpus {
		t.Error("claude-opus-4-6 missing from GetModels list")
	}
}

func TestGetProvidersIncludesKnown(t *testing.T) {
	withIsolatedModelRegistry(t)

	providers := GetProviders()
	sort.Slice(providers, func(i, j int) bool { return providers[i] < providers[j] })
	want := map[Provider]bool{
		ProviderAnthropic: true,
		ProviderOpenAI:    true,
		ProviderGoogle:    true,
	}
	for _, p := range providers {
		delete(want, p)
	}
	if len(want) > 0 {
		t.Errorf("missing providers: %v", want)
	}
}

func TestRegisterCustomModelOverrides(t *testing.T) {
	withIsolatedModelRegistry(t)

	custom := Model{
		ID:       "claude-opus-4-6",
		Name:     "Custom Opus",
		Api:      ApiAnthropicMessages,
		Provider: ProviderAnthropic,
		BaseURL:  "https://my-proxy.example.com",
		Cost:     ModelCost{Input: 0.5, Output: 2.0},
	}
	RegisterCustomModel(custom)

	m, ok := GetModel(ProviderAnthropic, "claude-opus-4-6")
	if !ok {
		t.Fatal("not found")
	}
	if m.Name != "Custom Opus" || m.BaseURL != "https://my-proxy.example.com" {
		t.Errorf("got %+v", m)
	}
	if m.Cost.Input != 0.5 {
		t.Errorf("custom cost ignored: %+v", m.Cost)
	}
}

func TestRegisterCustomModelNewModel(t *testing.T) {
	withIsolatedModelRegistry(t)

	custom := Model{
		ID:       "gcode-fake-model",
		Name:     "Fake",
		Api:      ApiOpenAICompletions,
		Provider: ProviderOpenAI,
		BaseURL:  "https://example.com",
	}
	RegisterCustomModel(custom)

	m, ok := GetModel(ProviderOpenAI, "gcode-fake-model")
	if !ok {
		t.Fatal("not found")
	}
	if m.Name != "Fake" {
		t.Errorf("name = %q", m.Name)
	}
}

// ---- helpers ----

// withIsolatedModelRegistry resets the package-level model registry around a
// test. It also points ModelsDevURL at an unusable URL and ModelCachePath at
// a temporary directory, so tests never hit the network and never clobber
// the user's ~/.gcode cache.
func withIsolatedModelRegistry(t *testing.T) {
	t.Helper()

	origURL := ModelsDevURL
	origPath := ModelCachePath
	ModelsDevURL = "http://127.0.0.1:1/should-fail"
	ModelCachePath = filepath.Join(t.TempDir(), "models.json")

	modelsMu.Lock()
	modelsLoaded = false
	baseModels = nil
	customModels = nil
	modelsMu.Unlock()

	t.Cleanup(func() {
		ModelsDevURL = origURL
		ModelCachePath = origPath
		modelsMu.Lock()
		modelsLoaded = false
		baseModels = nil
		customModels = nil
		modelsMu.Unlock()
	})
}

// ensure sync.WaitGroup is referenced so the race detector keeps the test
// file compilable even if later tests drop their only import.
var _ sync.WaitGroup
