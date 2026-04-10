package ai

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ----- CalculateCost -----

func TestCalculateCostBasic(t *testing.T) {
	m := Model{Cost: ModelCost{Input: 3.0, Output: 15.0, CacheRead: 0.3, CacheWrite: 3.75}}
	u := Usage{Input: 1_000_000, Output: 500_000, CacheRead: 100_000, CacheWrite: 200_000}
	got := CalculateCost(m, u)

	want := Cost{
		Input:      3.0,
		Output:     7.5,
		CacheRead:  0.03,
		CacheWrite: 0.75,
	}
	want.Total = want.Input + want.Output + want.CacheRead + want.CacheWrite

	if !floatEq(got.Input, want.Input) ||
		!floatEq(got.Output, want.Output) ||
		!floatEq(got.CacheRead, want.CacheRead) ||
		!floatEq(got.CacheWrite, want.CacheWrite) ||
		!floatEq(got.Total, want.Total) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestCalculateCostZeroUsage(t *testing.T) {
	m := Model{Cost: ModelCost{Input: 3.0, Output: 15.0}}
	got := CalculateCost(m, Usage{})
	if got != (Cost{}) {
		t.Errorf("zero usage should yield zero cost, got %+v", got)
	}
}

func TestCalculateCostSmallCounts(t *testing.T) {
	m := Model{Cost: ModelCost{Input: 3.0, Output: 15.0}}
	u := Usage{Input: 1, Output: 2}
	got := CalculateCost(m, u)
	// 1 token * $3/M = $0.000003
	if !floatEq(got.Input, 3e-6) {
		t.Errorf("input cost = %g", got.Input)
	}
	if !floatEq(got.Output, 30e-6) {
		t.Errorf("output cost = %g", got.Output)
	}
}

// ----- Model cache / fetch -----

func TestLoadModelCacheMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	_, err := loadModelCache(path)
	if err == nil {
		t.Error("expected error for missing cache file")
	}
}

func TestSaveAndLoadModelCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "models.json")
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	cache := &ModelCache{
		FetchedAt: now,
		Models: map[string]ModelData{
			"claude-opus-4-6": {
				ID:            "claude-opus-4-6",
				Name:          "Claude Opus 4.6",
				Provider:      "anthropic",
				ContextWindow: 200000,
				Cost:          ModelCost{Input: 15, Output: 75},
			},
		},
	}
	if err := saveModelCache(path, cache); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := loadModelCache(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !got.FetchedAt.Equal(now) {
		t.Errorf("FetchedAt = %v", got.FetchedAt)
	}
	m := got.Models["claude-opus-4-6"]
	if m.Name != "Claude Opus 4.6" || m.Cost.Input != 15 {
		t.Errorf("model = %+v", m)
	}
}

func TestIsCacheFresh(t *testing.T) {
	now := time.Now()
	fresh := &ModelCache{FetchedAt: now.Add(-1 * time.Hour)}
	stale := &ModelCache{FetchedAt: now.Add(-48 * time.Hour)}

	if !isCacheFresh(fresh, modelCacheTTL, now) {
		t.Error("1h old cache should be fresh")
	}
	if isCacheFresh(stale, modelCacheTTL, now) {
		t.Error("48h old cache should be stale")
	}
	if isCacheFresh(nil, modelCacheTTL, now) {
		t.Error("nil cache is not fresh")
	}
}

func TestFetchModelsFromURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := ModelCache{
			FetchedAt: time.Time{}, // server usually omits
			Models: map[string]ModelData{
				"m1": {ID: "m1", Name: "Model One", Provider: "openai", Cost: ModelCost{Input: 1.0}},
			},
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer server.Close()

	got, err := fetchModelsFromURL(server.URL, server.Client())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got == nil || got.Models["m1"].Name != "Model One" {
		t.Errorf("got %+v", got)
	}
}

func TestFetchModelsFromURLError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	if _, err := fetchModelsFromURL(server.URL, server.Client()); err == nil {
		t.Error("expected error on 500")
	}
}

func TestLoadOrFetchUsesFreshCache(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "models.json")
	cache := &ModelCache{
		FetchedAt: time.Now().Add(-1 * time.Hour),
		Models:    map[string]ModelData{"cached": {ID: "cached"}},
	}
	if err := saveModelCache(cachePath, cache); err != nil {
		t.Fatal(err)
	}

	// URL that should never be hit.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("fresh cache should not trigger a fetch")
	}))
	defer server.Close()

	got, src, err := loadOrFetchModels(server.URL, cachePath, server.Client(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if src != "cache" {
		t.Errorf("source = %q, want cache", src)
	}
	if _, ok := got.Models["cached"]; !ok {
		t.Errorf("expected cached data, got %+v", got.Models)
	}
}

func TestLoadOrFetchRefetchesStale(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "models.json")
	cache := &ModelCache{
		FetchedAt: time.Now().Add(-48 * time.Hour),
		Models:    map[string]ModelData{"old": {ID: "old"}},
	}
	if err := saveModelCache(cachePath, cache); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := ModelCache{Models: map[string]ModelData{"new": {ID: "new"}}}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer server.Close()

	got, src, err := loadOrFetchModels(server.URL, cachePath, server.Client(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if src != "network" {
		t.Errorf("source = %q, want network", src)
	}
	if _, ok := got.Models["new"]; !ok {
		t.Errorf("expected refreshed data, got %+v", got.Models)
	}

	// Cache on disk must now reflect the new data and a recent timestamp.
	reloaded, err := loadModelCache(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Models["new"]; !ok {
		t.Errorf("cache not updated on disk")
	}
	if time.Since(reloaded.FetchedAt) > time.Minute {
		t.Errorf("FetchedAt not updated: %v", reloaded.FetchedAt)
	}
}

func TestLoadOrFetchStaleFallbackOnNetworkError(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "models.json")
	cache := &ModelCache{
		FetchedAt: time.Now().Add(-48 * time.Hour),
		Models:    map[string]ModelData{"stale": {ID: "stale"}},
	}
	if err := saveModelCache(cachePath, cache); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	got, src, err := loadOrFetchModels(server.URL, cachePath, server.Client(), time.Now)
	if err != nil {
		t.Fatalf("should fall back to stale cache: %v", err)
	}
	if src != "stale-cache" {
		t.Errorf("source = %q, want stale-cache", src)
	}
	if _, ok := got.Models["stale"]; !ok {
		t.Errorf("expected stale cache data, got %+v", got.Models)
	}
}

func TestLoadOrFetchHardcodedFallback(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "does-not-exist.json")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	got, src, err := loadOrFetchModels(server.URL, cachePath, server.Client(), time.Now)
	if err != nil {
		t.Fatalf("should fall back to hardcoded defaults: %v", err)
	}
	if src != "fallback" {
		t.Errorf("source = %q, want fallback", src)
	}
	if len(got.Models) == 0 {
		t.Error("hardcoded fallback should have at least one model")
	}
}

func TestSaveModelCacheCreatesDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deep", "nested", "models.json")
	cache := &ModelCache{FetchedAt: time.Now(), Models: map[string]ModelData{"a": {ID: "a"}}}
	if err := saveModelCache(path, cache); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("cache file not created: %v", err)
	}
}

// ----- helpers -----

func floatEq(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}
