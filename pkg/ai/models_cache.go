package ai

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// ModelData describes a single model entry in the models.dev catalogue.
type ModelData struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Provider      string    `json:"provider"`
	ContextWindow int       `json:"contextWindow"`
	MaxTokens     int       `json:"maxTokens,omitempty"`
	Cost          ModelCost `json:"cost"`
}

// ModelCache is a time-stamped snapshot of the models.dev catalogue.
type ModelCache struct {
	FetchedAt time.Time            `json:"fetchedAt"`
	Models    map[string]ModelData `json:"models"`
}

const (
	// ModelsDevURL is the upstream catalogue endpoint. It is a package
	// variable so tests can point it at a httptest.Server.
	defaultModelsDevURL = "https://models.dev/api.json"

	// modelCacheTTL is how long a cache entry is considered fresh.
	modelCacheTTL = 24 * time.Hour
)

// ModelsDevURL is the URL used by LoadOrFetchModels. Exposed for tests.
var ModelsDevURL = defaultModelsDevURL

// LoadOrFetchModels returns the model catalogue, using a local cache when
// fresh and fetching from models.dev otherwise. On fetch failure it falls
// back to a stale cache, then to a hardcoded minimal default set.
func LoadOrFetchModels() (*ModelCache, error) {
	cache, _, err := loadOrFetchModels(ModelsDevURL, defaultCachePath(), http.DefaultClient, time.Now)
	return cache, err
}

// defaultCachePath returns ~/.gcode/cache/models.json. If the home directory
// cannot be determined, the returned path is "" and the caller will skip the
// on-disk cache entirely.
func defaultCachePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".gcode", "cache", "models.json")
}

// loadOrFetchModels is the injectable core of LoadOrFetchModels. The second
// return value reports where the data came from: "cache", "network",
// "stale-cache", or "fallback".
func loadOrFetchModels(url, cachePath string, client *http.Client, now func() time.Time) (*ModelCache, string, error) {
	if now == nil {
		now = time.Now
	}
	if client == nil {
		client = http.DefaultClient
	}

	var cached *ModelCache
	if cachePath != "" {
		if c, err := loadModelCache(cachePath); err == nil {
			cached = c
			if isCacheFresh(cached, modelCacheTTL, now()) {
				return cached, "cache", nil
			}
		}
	}

	fresh, err := fetchModelsFromURL(url, client)
	if err == nil {
		fresh.FetchedAt = now()
		if cachePath != "" {
			_ = saveModelCache(cachePath, fresh)
		}
		return fresh, "network", nil
	}

	if cached != nil {
		return cached, "stale-cache", nil
	}

	return hardcodedModelCache(now()), "fallback", nil
}

// loadModelCache reads a ModelCache from disk.
func loadModelCache(path string) (*ModelCache, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c ModelCache
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("ai: parse model cache %q: %w", path, err)
	}
	return &c, nil
}

// saveModelCache writes a ModelCache to disk, creating parent directories as
// needed.
func saveModelCache(path string, cache *ModelCache) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("ai: model cache dir: %w", err)
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("ai: encode model cache: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("ai: write model cache: %w", err)
	}
	return nil
}

// isCacheFresh reports whether cache was fetched within ttl of now.
func isCacheFresh(cache *ModelCache, ttl time.Duration, now time.Time) bool {
	if cache == nil {
		return false
	}
	return now.Sub(cache.FetchedAt) < ttl
}

// fetchModelsFromURL performs a GET and decodes a ModelCache.
func fetchModelsFromURL(url string, client *http.Client) (*ModelCache, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("ai: build model fetch request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ai: fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ai: fetch models: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ai: read model body: %w", err)
	}
	var c ModelCache
	if err := json.Unmarshal(body, &c); err != nil {
		return nil, fmt.Errorf("ai: decode model body: %w", err)
	}
	return &c, nil
}

// hardcodedModelCache returns a minimal built-in fallback used when no
// network and no cache are available. The list is intentionally small; the
// full catalogue comes from models.dev.
func hardcodedModelCache(now time.Time) *ModelCache {
	return &ModelCache{
		FetchedAt: now,
		Models: map[string]ModelData{
			"claude-opus-4-6": {
				ID:            "claude-opus-4-6",
				Name:          "Claude Opus 4.6",
				Provider:      string(ProviderAnthropic),
				ContextWindow: 200_000,
				MaxTokens:     32_000,
				Cost:          ModelCost{Input: 15, Output: 75, CacheRead: 1.5, CacheWrite: 18.75},
			},
			"claude-sonnet-4-6": {
				ID:            "claude-sonnet-4-6",
				Name:          "Claude Sonnet 4.6",
				Provider:      string(ProviderAnthropic),
				ContextWindow: 200_000,
				MaxTokens:     64_000,
				Cost:          ModelCost{Input: 3, Output: 15, CacheRead: 0.3, CacheWrite: 3.75},
			},
			"claude-haiku-4-5": {
				ID:            "claude-haiku-4-5",
				Name:          "Claude Haiku 4.5",
				Provider:      string(ProviderAnthropic),
				ContextWindow: 200_000,
				MaxTokens:     32_000,
				Cost:          ModelCost{Input: 1, Output: 5, CacheRead: 0.1, CacheWrite: 1.25},
			},
			"gpt-4.1": {
				ID:            "gpt-4.1",
				Name:          "GPT-4.1",
				Provider:      string(ProviderOpenAI),
				ContextWindow: 128_000,
				MaxTokens:     16_384,
				Cost:          ModelCost{Input: 2.5, Output: 10, CacheRead: 1.25},
			},
			"gemini-2.5-pro": {
				ID:            "gemini-2.5-pro",
				Name:          "Gemini 2.5 Pro",
				Provider:      string(ProviderGoogle),
				ContextWindow: 1_000_000,
				MaxTokens:     8_192,
				Cost:          ModelCost{Input: 1.25, Output: 10},
			},
		},
	}
}
