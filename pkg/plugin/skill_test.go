package plugin

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func writeSkillFile(t *testing.T, path, content string) {
	t.Helper()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadSkillsFromSkillMdDirectories(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, filepath.Join(dir, "git", "SKILL.md"), `---
name: git
description: Manage git workflows
---
Use git commands effectively.`)
	writeSkillFile(t, filepath.Join(dir, "docker", "SKILL.md"), `---
name: docker
description: Container workflows
---
Docker body.`)

	skills, err := LoadSkills(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 2 {
		t.Fatalf("got %d skills", len(skills))
	}

	names := []string{skills[0].Name, skills[1].Name}
	sort.Strings(names)
	if names[0] != "docker" || names[1] != "git" {
		t.Errorf("names = %v", names)
	}
}

func TestLoadSkillsFlatFile(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, filepath.Join(dir, "custom.skill.md"), `---
name: custom
description: Custom skill
---
body here`)

	skills, err := LoadSkills(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 {
		t.Fatalf("got %d", len(skills))
	}
	if skills[0].Name != "custom" {
		t.Errorf("name = %q", skills[0].Name)
	}
}

func TestLoadSkillsMissingDir(t *testing.T) {
	skills, err := LoadSkills(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatal(err)
	}
	if skills != nil {
		t.Errorf("expected nil, got %v", skills)
	}
}

func TestLoadSkillsEmptyPath(t *testing.T) {
	skills, err := LoadSkills("")
	if err != nil || skills != nil {
		t.Errorf("err=%v skills=%v", err, skills)
	}
}

func TestLoadSkillsIgnoresNonSkillFiles(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, filepath.Join(dir, "README.md"), "not a skill")
	writeSkillFile(t, filepath.Join(dir, "sub", "SKILL.md"), "---\nname: keep\n---\nbody")

	skills, err := LoadSkills(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 || skills[0].Name != "keep" {
		t.Errorf("got %+v", skills)
	}
}

func TestSkillFrontmatterTrigger(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, filepath.Join(dir, "git", "SKILL.md"), `---
name: git
trigger: commit, push, branch
---
body`)

	skills, err := LoadSkills(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 {
		t.Fatal()
	}
	if len(skills[0].Trigger) != 3 {
		t.Errorf("trigger = %v", skills[0].Trigger)
	}
}

func TestSkillBodyStripsFrontmatter(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, filepath.Join(dir, "x", "SKILL.md"), `---
name: x
---
pure body text`)
	skills, _ := LoadSkills(dir)
	if skills[0].Body != "pure body text" {
		t.Errorf("body = %q", skills[0].Body)
	}
}

func TestSkillNoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, filepath.Join(dir, "naked", "SKILL.md"), "just body")
	skills, _ := LoadSkills(dir)
	if len(skills) != 1 {
		t.Fatal()
	}
	if skills[0].Name != "naked" {
		t.Errorf("derived name = %q", skills[0].Name)
	}
	if skills[0].Body != "just body" {
		t.Errorf("body = %q", skills[0].Body)
	}
}
