package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunDiscoveryEmpty(t *testing.T) {
	d := RunDiscovery(DiscoveryPaths{Dirs: []string{t.TempDir()}})
	if d.ProjectContext != "" {
		t.Errorf("empty discovery should have no context, got %q", d.ProjectContext)
	}
	if len(d.Skills) != 0 {
		t.Errorf("empty discovery should have no skills")
	}
}

func TestRunDiscoveryProjectAgentsMD(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("project instructions"), 0o644)

	d := RunDiscovery(DiscoveryPaths{Dirs: []string{dir}})
	if !strings.Contains(d.ProjectContext, "project instructions") {
		t.Errorf("got %q", d.ProjectContext)
	}
}

func TestRunDiscoveryProjectBeforeGlobal(t *testing.T) {
	project := t.TempDir()
	global := t.TempDir()
	os.WriteFile(filepath.Join(project, "AGENTS.md"), []byte("PROJECT"), 0o644)
	os.WriteFile(filepath.Join(global, "AGENTS.md"), []byte("GLOBAL"), 0o644)

	d := RunDiscovery(DiscoveryPaths{Dirs: []string{project, global}})
	if !strings.HasPrefix(d.ProjectContext, "PROJECT") {
		t.Errorf("project should come first: %q", d.ProjectContext)
	}
	if !strings.Contains(d.ProjectContext, "GLOBAL") {
		t.Errorf("global missing: %q", d.ProjectContext)
	}
}

func TestRunDiscoverySkillsMerged(t *testing.T) {
	project := t.TempDir()
	global := t.TempDir()
	os.MkdirAll(filepath.Join(project, "skills", "git"), 0o755)
	os.MkdirAll(filepath.Join(global, "skills", "docker"), 0o755)
	os.WriteFile(filepath.Join(project, "skills", "git", "SKILL.md"), []byte("---\nname: git\n---\nbody"), 0o644)
	os.WriteFile(filepath.Join(global, "skills", "docker", "SKILL.md"), []byte("---\nname: docker\n---\nbody"), 0o644)

	d := RunDiscovery(DiscoveryPaths{Dirs: []string{project, global}})
	if len(d.Skills) != 2 {
		t.Errorf("skills = %d", len(d.Skills))
	}
}

func TestDefaultDiscoveryPaths(t *testing.T) {
	paths := DefaultDiscoveryPaths("/project")
	// Should have at least .gcode and .agents in project, and probably
	// ~/.gcode / ~/.agents.
	if len(paths.Dirs) < 2 {
		t.Errorf("got %d dirs", len(paths.Dirs))
	}
	if paths.Dirs[0] != filepath.Join("/project", ".gcode") {
		t.Errorf("first = %q", paths.Dirs[0])
	}
}

func TestRunDiscoveryReadsCLAUDEMDFallback(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("claude instructions"), 0o644)
	d := RunDiscovery(DiscoveryPaths{Dirs: []string{dir}})
	if !strings.Contains(d.ProjectContext, "claude instructions") {
		t.Errorf("got %q", d.ProjectContext)
	}
}
