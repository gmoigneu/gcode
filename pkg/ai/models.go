package ai

import (
	"sync"
	"time"
)

// ModelCachePath is the on-disk path used by LoadOrFetchModels. When empty,
// ~/.gcode/cache/models.json is used. Tests may override this to redirect to
// a t.TempDir().
var ModelCachePath string

var (
	modelsMu     sync.RWMutex
	modelsLoaded bool
	baseModels   map[Provider]map[string]Model
	customModels map[Provider]map[string]Model
)

// GetModel returns a specific model by provider and ID. The lookup order
// is: custom (RegisterCustomModel) > models.dev / on-disk cache > hardcoded
// fallback.
func GetModel(provider Provider, id string) (Model, bool) {
	ensureModelsLoaded()

	modelsMu.RLock()
	defer modelsMu.RUnlock()
	if m, ok := customModels[provider][id]; ok {
		return m, true
	}
	if m, ok := baseModels[provider][id]; ok {
		return m, true
	}
	return Model{}, false
}

// GetModels returns every model registered under a provider, with custom
// entries taking precedence over base entries with the same ID.
func GetModels(provider Provider) []Model {
	ensureModelsLoaded()

	modelsMu.RLock()
	defer modelsMu.RUnlock()

	seen := map[string]bool{}
	var out []Model
	for id, m := range customModels[provider] {
		seen[id] = true
		out = append(out, m)
	}
	for id, m := range baseModels[provider] {
		if seen[id] {
			continue
		}
		out = append(out, m)
	}
	return out
}

// GetProviders returns the set of providers known to the registry (union of
// custom + base).
func GetProviders() []Provider {
	ensureModelsLoaded()

	modelsMu.RLock()
	defer modelsMu.RUnlock()

	seen := map[Provider]bool{}
	for p := range baseModels {
		seen[p] = true
	}
	for p := range customModels {
		seen[p] = true
	}
	out := make([]Provider, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	return out
}

// RegisterCustomModel publishes a user-defined model that overrides any
// base entry with the same (provider, id).
func RegisterCustomModel(m Model) {
	modelsMu.Lock()
	defer modelsMu.Unlock()
	if customModels == nil {
		customModels = map[Provider]map[string]Model{}
	}
	if customModels[m.Provider] == nil {
		customModels[m.Provider] = map[string]Model{}
	}
	customModels[m.Provider][m.ID] = m
}

// ensureModelsLoaded lazy-initialises baseModels. It loads the hardcoded
// defaults and overlays the models.dev catalogue (via LoadOrFetchModels).
// Failures in the models.dev fetch are ignored — the hardcoded fallback
// always remains.
func ensureModelsLoaded() {
	modelsMu.RLock()
	if modelsLoaded {
		modelsMu.RUnlock()
		return
	}
	modelsMu.RUnlock()

	modelsMu.Lock()
	defer modelsMu.Unlock()
	if modelsLoaded {
		return
	}
	baseModels = loadBaseModels()
	modelsLoaded = true
}

// loadBaseModels merges hardcoded + models.dev entries. models.dev wins on
// conflict; hardcoded fills in everything else.
func loadBaseModels() map[Provider]map[string]Model {
	out := map[Provider]map[string]Model{}

	// Hardcoded defaults.
	for _, md := range hardcodedModelCache(time.Now()).Models {
		addModelData(out, md)
	}

	// Overlay models.dev catalogue, if available.
	if cache, err := LoadOrFetchModels(); err == nil && cache != nil {
		for _, md := range cache.Models {
			addModelData(out, md)
		}
	}
	return out
}

func addModelData(dst map[Provider]map[string]Model, md ModelData) {
	m := modelDataToModel(md)
	if m.Provider == "" {
		return
	}
	if dst[m.Provider] == nil {
		dst[m.Provider] = map[string]Model{}
	}
	dst[m.Provider][m.ID] = m
}

// modelDataToModel converts a models.dev / hardcoded entry into a full Model
// with API and default base URL filled in from the provider.
func modelDataToModel(md ModelData) Model {
	p := Provider(md.Provider)
	api, baseURL := defaultsForProvider(p)
	return Model{
		ID:            md.ID,
		Name:          md.Name,
		Api:           api,
		Provider:      p,
		BaseURL:       baseURL,
		Input:         []string{"text"},
		Cost:          md.Cost,
		ContextWindow: md.ContextWindow,
		MaxTokens:     md.MaxTokens,
	}
}

// defaultsForProvider returns the default Api and BaseURL for a provider.
// These defaults can be overridden by custom Model entries or by
// provider-specific configuration.
func defaultsForProvider(p Provider) (Api, string) {
	switch p {
	case ProviderAnthropic:
		return ApiAnthropicMessages, "https://api.anthropic.com/v1"
	case ProviderGoogle:
		return ApiGoogleGemini, "https://generativelanguage.googleapis.com/v1beta"
	case ProviderOpenAI:
		return ApiOpenAICompletions, "https://api.openai.com/v1"
	case ProviderGroq:
		return ApiOpenAICompletions, "https://api.groq.com/openai/v1"
	case ProviderXAI:
		return ApiOpenAICompletions, "https://api.x.ai/v1"
	case ProviderCerebras:
		return ApiOpenAICompletions, "https://api.cerebras.ai/v1"
	case ProviderOpenRouter:
		return ApiOpenAICompletions, "https://openrouter.ai/api/v1"
	}
	return ApiOpenAICompletions, ""
}
