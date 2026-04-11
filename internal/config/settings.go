// Package config holds gcode's user-facing configuration: settings and
// auth storage. All files are JSON on disk for easy manual editing.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gmoigneu/gcode/pkg/ai"
)

// Settings holds per-user gcode preferences. Project-local settings
// layer on top of global settings and win on conflict.
type Settings struct {
	DefaultModel    string     `json:"defaultModel,omitempty"`
	DefaultProvider string     `json:"defaultProvider,omitempty"`
	ThinkingLevel   string     `json:"thinkingLevel,omitempty"`
	CustomModels    []ai.Model `json:"customModels,omitempty"`
}

// SettingsPaths resolves the two canonical settings file locations:
// global (~/.gcode/settings.json) and project (.gcode/settings.json).
type SettingsPaths struct {
	Global  string
	Project string
}

// DefaultSettingsPaths returns the conventional settings paths. When
// cwd is empty the project path is also empty (meaning "no project").
func DefaultSettingsPaths(cwd string) SettingsPaths {
	var paths SettingsPaths
	if home, err := os.UserHomeDir(); err == nil {
		paths.Global = filepath.Join(home, ".gcode", "settings.json")
	}
	if cwd != "" {
		paths.Project = filepath.Join(cwd, ".gcode", "settings.json")
	}
	return paths
}

// LoadSettings reads global + project settings and merges them.
// Project values overlay global values; unset fields inherit from
// global.
func LoadSettings(paths SettingsPaths) Settings {
	global := readSettings(paths.Global)
	project := readSettings(paths.Project)
	return mergeSettings(global, project)
}

// SaveSettings writes s to path. Creates parent directories as needed.
func SaveSettings(path string, s Settings) error {
	if path == "" {
		return errors.New("config: SaveSettings: empty path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("config: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("config: marshal settings: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("config: write settings: %w", err)
	}
	return nil
}

func readSettings(path string) Settings {
	if path == "" {
		return Settings{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Settings{}
	}
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return Settings{}
	}
	return s
}

func mergeSettings(global, project Settings) Settings {
	out := global
	if project.DefaultModel != "" {
		out.DefaultModel = project.DefaultModel
	}
	if project.DefaultProvider != "" {
		out.DefaultProvider = project.DefaultProvider
	}
	if project.ThinkingLevel != "" {
		out.ThinkingLevel = project.ThinkingLevel
	}
	if len(project.CustomModels) > 0 {
		out.CustomModels = append(out.CustomModels, project.CustomModels...)
	}
	return out
}
