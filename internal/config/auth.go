package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gmoigneu/gcode/pkg/ai"
)

// AuthStorage holds provider → API key mappings. Backed by a JSON file
// at ~/.gcode/auth.json.
type AuthStorage struct {
	Keys map[string]string `json:"keys"`
}

// DefaultAuthPath returns ~/.gcode/auth.json. Returns "" if the home
// directory cannot be resolved.
func DefaultAuthPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".gcode", "auth.json")
}

// LoadAuth reads the auth storage from path. Missing files return an
// empty storage with no error.
func LoadAuth(path string) AuthStorage {
	if path == "" {
		return AuthStorage{Keys: map[string]string{}}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return AuthStorage{Keys: map[string]string{}}
	}
	var a AuthStorage
	if err := json.Unmarshal(data, &a); err != nil {
		return AuthStorage{Keys: map[string]string{}}
	}
	if a.Keys == nil {
		a.Keys = map[string]string{}
	}
	return a
}

// SaveAuth writes a to path with 0600 permissions (tighter than
// settings.json because it holds secrets).
func SaveAuth(path string, a AuthStorage) error {
	if path == "" {
		return errors.New("config: SaveAuth: empty path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("config: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return fmt.Errorf("config: marshal auth: %w", err)
	}
	return os.WriteFile(path, data, 0o600)
}

// envVarForProvider returns the conventional environment variable name
// used to look up a provider's API key.
func envVarForProvider(p ai.Provider) string {
	switch p {
	case ai.ProviderAnthropic:
		return "ANTHROPIC_API_KEY"
	case ai.ProviderOpenAI:
		return "OPENAI_API_KEY"
	case ai.ProviderGoogle:
		return "GOOGLE_API_KEY"
	case ai.ProviderGroq:
		return "GROQ_API_KEY"
	case ai.ProviderXAI:
		return "XAI_API_KEY"
	case ai.ProviderCerebras:
		return "CEREBRAS_API_KEY"
	case ai.ProviderOpenRouter:
		return "OPENROUTER_API_KEY"
	}
	return ""
}

// GetAPIKey returns the key for provider. It first checks the stored
// keys, then falls back to the conventional environment variable.
func (a AuthStorage) GetAPIKey(provider ai.Provider) string {
	if a.Keys != nil {
		if key, ok := a.Keys[string(provider)]; ok && key != "" {
			return key
		}
	}
	if env := envVarForProvider(provider); env != "" {
		return os.Getenv(env)
	}
	return ""
}

// SetAPIKey records a key in memory. Callers must invoke SaveAuth to
// persist.
func (a *AuthStorage) SetAPIKey(provider ai.Provider, key string) {
	if a.Keys == nil {
		a.Keys = map[string]string{}
	}
	a.Keys[string(provider)] = key
}
