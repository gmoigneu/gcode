package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gmoigneu/gcode/pkg/ai"
)

// ----- Settings -----

func TestLoadSettingsMissing(t *testing.T) {
	s := LoadSettings(SettingsPaths{Global: filepath.Join(t.TempDir(), "missing.json")})
	if s.DefaultModel != "" {
		t.Errorf("expected empty, got %+v", s)
	}
}

func TestSaveLoadSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "settings.json")
	s := Settings{
		DefaultModel:    "claude-opus-4-6",
		DefaultProvider: "anthropic",
		ThinkingLevel:   "high",
	}
	if err := SaveSettings(path, s); err != nil {
		t.Fatal(err)
	}
	got := LoadSettings(SettingsPaths{Global: path})
	if got.DefaultModel != "claude-opus-4-6" || got.ThinkingLevel != "high" {
		t.Errorf("got %+v", got)
	}
}

func TestLoadSettingsProjectOverlay(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "global.json")
	project := filepath.Join(dir, "project.json")

	if err := SaveSettings(global, Settings{
		DefaultModel:    "claude",
		DefaultProvider: "anthropic",
		ThinkingLevel:   "medium",
	}); err != nil {
		t.Fatal(err)
	}
	if err := SaveSettings(project, Settings{
		DefaultModel: "gpt-4",
	}); err != nil {
		t.Fatal(err)
	}
	got := LoadSettings(SettingsPaths{Global: global, Project: project})
	if got.DefaultModel != "gpt-4" {
		t.Errorf("project should override: %+v", got)
	}
	// Global thinking level should survive.
	if got.ThinkingLevel != "medium" {
		t.Errorf("global should survive: %+v", got)
	}
}

func TestLoadSettingsCustomModelsAppend(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "global.json")
	project := filepath.Join(dir, "project.json")

	SaveSettings(global, Settings{CustomModels: []ai.Model{{ID: "g1"}}})
	SaveSettings(project, Settings{CustomModels: []ai.Model{{ID: "p1"}}})

	got := LoadSettings(SettingsPaths{Global: global, Project: project})
	if len(got.CustomModels) != 2 {
		t.Errorf("got %d models", len(got.CustomModels))
	}
}

func TestSaveSettingsEmptyPath(t *testing.T) {
	if err := SaveSettings("", Settings{}); err == nil {
		t.Error("expected error")
	}
}

func TestDefaultSettingsPaths(t *testing.T) {
	paths := DefaultSettingsPaths("/project")
	if paths.Project == "" {
		t.Error("project path should be set")
	}
}

// ----- Auth -----

func TestSaveLoadAuth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	a := AuthStorage{Keys: map[string]string{"anthropic": "sk-abc"}}
	if err := SaveAuth(path, a); err != nil {
		t.Fatal(err)
	}
	got := LoadAuth(path)
	if got.Keys["anthropic"] != "sk-abc" {
		t.Errorf("got %+v", got)
	}
}

func TestLoadAuthMissing(t *testing.T) {
	got := LoadAuth(filepath.Join(t.TempDir(), "missing.json"))
	if got.Keys == nil {
		t.Error("keys map should be initialised")
	}
}

func TestAuthGetAPIKeyStored(t *testing.T) {
	a := AuthStorage{Keys: map[string]string{"anthropic": "stored-key"}}
	if got := a.GetAPIKey(ai.ProviderAnthropic); got != "stored-key" {
		t.Errorf("got %q", got)
	}
}

func TestAuthGetAPIKeyEnvFallback(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "env-key")
	a := AuthStorage{}
	if got := a.GetAPIKey(ai.ProviderOpenAI); got != "env-key" {
		t.Errorf("got %q", got)
	}
}

func TestAuthGetAPIKeyUnknown(t *testing.T) {
	a := AuthStorage{}
	if got := a.GetAPIKey("unknown"); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestAuthSetAPIKey(t *testing.T) {
	var a AuthStorage
	a.SetAPIKey(ai.ProviderGoogle, "new-key")
	if a.Keys["google"] != "new-key" {
		t.Errorf("got %v", a.Keys)
	}
}

func TestSaveAuthEmptyPath(t *testing.T) {
	if err := SaveAuth("", AuthStorage{}); err == nil {
		t.Error("expected error")
	}
}

func TestDefaultAuthPath(t *testing.T) {
	if DefaultAuthPath() == "" {
		t.Error("default auth path should not be empty in a normal env")
	}
}

func TestSaveAuthPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := SaveAuth(path, AuthStorage{Keys: map[string]string{"x": "y"}}); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	// Permissions may be affected by umask; we only check it's not
	// world-readable.
	if info.Mode().Perm()&0o004 != 0 {
		t.Errorf("auth file is world-readable: %v", info.Mode())
	}
}
