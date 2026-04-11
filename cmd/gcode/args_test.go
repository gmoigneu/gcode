package main

import (
	"testing"

	"github.com/gmoigneu/gcode/internal/config"
	"github.com/gmoigneu/gcode/pkg/ai"
)

func TestParseArgsPrompt(t *testing.T) {
	a, ok := ParseArgs([]string{"-p", "hello"})
	if !ok {
		t.Fatal("parse failed")
	}
	if a.Prompt != "hello" {
		t.Errorf("got %q", a.Prompt)
	}
}

func TestParseArgsTrailingPrompt(t *testing.T) {
	a, ok := ParseArgs([]string{"hello", "world"})
	if !ok || a.Prompt != "hello world" {
		t.Errorf("got %+v", a)
	}
}

func TestParseArgsModelShort(t *testing.T) {
	a, _ := ParseArgs([]string{"-m", "gpt-4"})
	if a.Model != "gpt-4" {
		t.Errorf("got %+v", a)
	}
}

func TestParseArgsPrintMode(t *testing.T) {
	a, _ := ParseArgs([]string{"--print", "-p", "hi"})
	if !a.PrintMode {
		t.Errorf("print not set")
	}
}

func TestParseArgsHelp(t *testing.T) {
	_, ok := ParseArgs([]string{"--help"})
	if ok {
		t.Errorf("help should return ok=false")
	}
}

func TestParseArgsInvalid(t *testing.T) {
	_, ok := ParseArgs([]string{"--nope"})
	if ok {
		t.Errorf("invalid flag should return ok=false")
	}
}

func TestParseArgsThinking(t *testing.T) {
	a, _ := ParseArgs([]string{"--thinking", "high"})
	if a.ThinkingLevel != "high" {
		t.Errorf("got %q", a.ThinkingLevel)
	}
}

func TestResolveModelProviderSlashID(t *testing.T) {
	// The model registry is populated lazily in the ai package; use a
	// known hardcoded fallback entry.
	_, err := ResolveModel(Args{Model: "anthropic/claude-opus-4-6"})
	if err != nil {
		t.Errorf("err = %v", err)
	}
}

func TestResolveModelBareID(t *testing.T) {
	_, err := ResolveModel(Args{Model: "claude-opus-4-6"})
	if err != nil {
		t.Errorf("err = %v", err)
	}
}

func TestResolveModelMissing(t *testing.T) {
	_, err := ResolveModel(Args{Model: "does-not-exist"})
	if err == nil {
		t.Error("expected error")
	}
}

func TestResolveModelEmpty(t *testing.T) {
	_, err := ResolveModel(Args{})
	if err == nil {
		t.Error("expected error")
	}
}

func TestResolveAPIKeyFromFlag(t *testing.T) {
	got := ResolveAPIKey(Args{APIKey: "flag-key"}, ai.Model{}, config.AuthStorage{})
	if got != "flag-key" {
		t.Errorf("got %q", got)
	}
}

func TestResolveAPIKeyFromStorage(t *testing.T) {
	auth := config.AuthStorage{Keys: map[string]string{"anthropic": "stored-key"}}
	got := ResolveAPIKey(Args{}, ai.Model{Provider: ai.ProviderAnthropic}, auth)
	if got != "stored-key" {
		t.Errorf("got %q", got)
	}
}

func TestResolveAPIKeyFromEnv(t *testing.T) {
	t.Setenv("GOOGLE_API_KEY", "env-key")
	got := ResolveAPIKey(Args{}, ai.Model{Provider: ai.ProviderGoogle}, config.AuthStorage{})
	if got != "env-key" {
		t.Errorf("got %q", got)
	}
}
