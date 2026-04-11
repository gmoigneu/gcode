package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gmoigneu/gcode/pkg/agent"
	"github.com/gmoigneu/gcode/pkg/ai"
)

var fixedNow = func() time.Time { return time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC) }

func TestBuildSystemPromptMinimal(t *testing.T) {
	got := BuildSystemPrompt(Options{Now: fixedNow})
	if !strings.Contains(got, "gcode") {
		t.Errorf("role declaration missing: %q", got)
	}
	if !strings.Contains(got, "2026-04-10") {
		t.Errorf("date missing: %q", got)
	}
}

func TestBuildSystemPromptWithTools(t *testing.T) {
	tools := []agent.AgentTool{
		{Tool: ai.Tool{Name: "read", Description: "Read a file"}},
		{Tool: ai.Tool{Name: "bash", Description: "Run shell commands"}},
	}
	got := BuildSystemPrompt(Options{Tools: tools, Now: fixedNow})
	if !strings.Contains(got, "**read**") || !strings.Contains(got, "Read a file") {
		t.Errorf("tool not listed: %q", got)
	}
	if !strings.Contains(got, "**bash**") {
		t.Errorf("bash tool missing: %q", got)
	}
	if !strings.Contains(got, "Guidelines") {
		t.Errorf("guidelines missing: %q", got)
	}
}

func TestBuildSystemPromptWithSkills(t *testing.T) {
	skills := []SkillSummary{
		{Name: "git", Description: "Manage git workflows"},
	}
	got := BuildSystemPrompt(Options{Skills: skills, Now: fixedNow})
	if !strings.Contains(got, "Skills") || !strings.Contains(got, "git") {
		t.Errorf("skill missing: %q", got)
	}
}

func TestBuildSystemPromptWithProjectContext(t *testing.T) {
	got := BuildSystemPrompt(Options{ProjectContext: "Use Go 1.24+", Now: fixedNow})
	if !strings.Contains(got, "Project context") {
		t.Errorf("section missing: %q", got)
	}
	if !strings.Contains(got, "Use Go 1.24+") {
		t.Errorf("body missing: %q", got)
	}
}

func TestBuildSystemPromptCwd(t *testing.T) {
	got := BuildSystemPrompt(Options{Cwd: "/tmp/test", Now: fixedNow})
	if !strings.Contains(got, "/tmp/test") {
		t.Errorf("cwd missing: %q", got)
	}
}

func TestLoadProjectContextAgentsMD(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("project instructions"), 0o644)
	got := LoadProjectContext(dir)
	if got != "project instructions" {
		t.Errorf("got %q", got)
	}
}

func TestLoadProjectContextCLAUDEMD(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("claude instructions"), 0o644)
	got := LoadProjectContext(dir)
	if got != "claude instructions" {
		t.Errorf("got %q", got)
	}
}

func TestLoadProjectContextMissing(t *testing.T) {
	dir := t.TempDir()
	if got := LoadProjectContext(dir); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestLoadProjectContextEmptyCwd(t *testing.T) {
	if got := LoadProjectContext(""); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestLoadProjectContextAgentsDir(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".agents"), 0o755)
	os.WriteFile(filepath.Join(dir, ".agents", "AGENTS.md"), []byte("subdir"), 0o644)
	got := LoadProjectContext(dir)
	if got != "subdir" {
		t.Errorf("got %q", got)
	}
}
