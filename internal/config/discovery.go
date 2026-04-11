package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/gmoigneu/gcode/pkg/plugin"
)

// Discovery holds everything loaded from user configuration
// directories: the project context files and the skill directories.
type Discovery struct {
	// ProjectContext is the concatenated content of any AGENTS.md files
	// found in the precedence order (project → global).
	ProjectContext string

	// Skills is the merged list from global and project skill directories.
	Skills []plugin.Skill
}

// DiscoveryPaths are the canonical gcode / agents directories we look
// in. The full list (in precedence order, project first):
//
//  1. $PWD/.gcode
//  2. $PWD/.agents
//  3. ~/.gcode
//  4. ~/.agents
type DiscoveryPaths struct {
	Dirs []string
}

// DefaultDiscoveryPaths returns the conventional discovery paths for a
// given working directory.
func DefaultDiscoveryPaths(cwd string) DiscoveryPaths {
	var dirs []string
	if cwd != "" {
		dirs = append(dirs, filepath.Join(cwd, ".gcode"))
		dirs = append(dirs, filepath.Join(cwd, ".agents"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".gcode"))
		dirs = append(dirs, filepath.Join(home, ".agents"))
	}
	return DiscoveryPaths{Dirs: dirs}
}

// RunDiscovery loads AGENTS.md files and skills from the configured
// directories. Project files take precedence and appear first in the
// concatenated project context.
func RunDiscovery(paths DiscoveryPaths) Discovery {
	var d Discovery
	var contextParts []string

	for _, dir := range paths.Dirs {
		if dir == "" {
			continue
		}
		// AGENTS.md (or CLAUDE.md) directly in the directory.
		if body := readFirst(filepath.Join(dir, "AGENTS.md"), filepath.Join(dir, "CLAUDE.md")); body != "" {
			contextParts = append(contextParts, body)
		}
		// Skills subdirectory.
		skillsDir := filepath.Join(dir, "skills")
		if skills, _ := plugin.LoadSkills(skillsDir); len(skills) > 0 {
			d.Skills = append(d.Skills, skills...)
		}
	}
	d.ProjectContext = strings.Join(contextParts, "\n\n")
	return d
}

// readFirst returns the content of the first existing file in paths.
func readFirst(paths ...string) string {
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err == nil {
			return strings.TrimSpace(string(data))
		}
	}
	return ""
}
