// Package plugin implements gcode's extensibility: skills (markdown
// instruction files) and subprocess/RPC plugin hosts.
package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Skill is a single markdown instruction file loaded from a skills
// directory. The body is the prose the system prompt will include; the
// frontmatter holds metadata.
type Skill struct {
	Name        string
	Description string
	Trigger     []string
	Body        string
	Path        string
}

// LoadSkills walks dir and returns every skill it finds. A skill is a
// file named SKILL.md (or any *.md if flat) containing an optional YAML
// frontmatter block delimited by "---". Missing directories are not
// errors (returns an empty slice).
func LoadSkills(dir string) ([]Skill, error) {
	if dir == "" {
		return nil, nil
	}
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("plugin: stat skills dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("plugin: skills path %q is not a directory", dir)
	}

	var skills []Skill
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // tolerate unreadable files
		}
		if info.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if base != "SKILL.md" && !strings.HasSuffix(base, ".skill.md") {
			return nil
		}
		skill, err := loadSkillFile(path)
		if err != nil {
			return nil // tolerate bad files
		}
		skills = append(skills, skill)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return skills, nil
}

// loadSkillFile parses a single skill file.
func loadSkillFile(path string) (Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, err
	}
	text := string(data)

	skill := Skill{Path: path, Name: defaultName(path)}

	// Parse YAML-ish frontmatter: "---\nkey: value\n---\n<body>".
	if strings.HasPrefix(text, "---\n") {
		end := strings.Index(text[4:], "\n---")
		if end > 0 {
			frontmatter := text[4 : 4+end]
			skill.Body = strings.TrimLeft(text[4+end+4:], "\n")
			applyFrontmatter(&skill, frontmatter)
		} else {
			skill.Body = text
		}
	} else {
		skill.Body = text
	}
	if skill.Name == "" {
		skill.Name = defaultName(path)
	}
	return skill, nil
}

// defaultName derives a sensible name from the file path — the parent
// directory for SKILL.md, or the basename for *.skill.md.
func defaultName(path string) string {
	base := filepath.Base(path)
	if base == "SKILL.md" {
		return filepath.Base(filepath.Dir(path))
	}
	return strings.TrimSuffix(base, ".skill.md")
}

// applyFrontmatter fills skill fields from "key: value" lines. Only
// name, description, and trigger are recognised; unknown keys are
// ignored.
func applyFrontmatter(s *Skill, yaml string) {
	for _, line := range strings.Split(yaml, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		val = strings.Trim(val, `"'`)
		switch strings.ToLower(key) {
		case "name":
			s.Name = val
		case "description":
			s.Description = val
		case "trigger", "triggers":
			// Accept comma- or space-separated list.
			val = strings.Trim(val, "[]")
			parts := strings.FieldsFunc(val, func(r rune) bool { return r == ',' || r == ' ' })
			for _, p := range parts {
				p = strings.Trim(p, `"'`)
				if p != "" {
					s.Trigger = append(s.Trigger, p)
				}
			}
		}
	}
}
