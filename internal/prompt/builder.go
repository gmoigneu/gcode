// Package prompt builds the system prompt sent to the LLM from the
// agent configuration (tools, cwd, AGENTS.md overrides, optional skills).
package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gmoigneu/gcode/pkg/agent"
)

// Options configures the system prompt builder.
type Options struct {
	// Cwd is the working directory announced to the model.
	Cwd string

	// Tools are the tools available to the agent. Their names and
	// descriptions are rendered as a bullet list.
	Tools []agent.AgentTool

	// ProjectContext is optional free-form text — typically the contents
	// of an AGENTS.md / CLAUDE.md file — appended verbatim.
	ProjectContext string

	// Skills are optional named skill descriptions appended after the
	// tool list so the model knows what extra capabilities are available.
	Skills []SkillSummary

	// Now is injected for deterministic tests. When nil, time.Now is used.
	Now func() time.Time
}

// SkillSummary is the minimum the builder needs from each skill.
type SkillSummary struct {
	Name        string
	Description string
}

// BuildSystemPrompt assembles the complete system prompt.
func BuildSystemPrompt(opts Options) string {
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	var b strings.Builder

	b.WriteString("You are gcode, an expert AI coding assistant running in a terminal.\n")
	b.WriteString("Be concise, accurate, and direct. Prefer running tools over speculating.\n\n")

	if opts.Cwd != "" {
		fmt.Fprintf(&b, "Working directory: %s\n", opts.Cwd)
	}
	fmt.Fprintf(&b, "Current date: %s\n\n", now().UTC().Format("2006-01-02"))

	if len(opts.Tools) > 0 {
		b.WriteString("## Tools\n\n")
		b.WriteString("You have access to the following tools:\n\n")
		for _, t := range opts.Tools {
			desc := strings.TrimSpace(t.Tool.Description)
			if desc == "" {
				desc = "(no description)"
			}
			fmt.Fprintf(&b, "- **%s**: %s\n", t.Tool.Name, desc)
		}
		b.WriteString("\n")
		b.WriteString("Guidelines:\n")
		b.WriteString("- Use `bash` for quick shell operations (ls, grep, find, etc.).\n")
		b.WriteString("- Use `read` to inspect files before modifying them.\n")
		b.WriteString("- Use `edit` for precise targeted changes — `oldText` must match exactly.\n")
		b.WriteString("- Use `write` only for brand-new files or complete rewrites.\n")
		b.WriteString("- Use `fetch` for HTTP requests instead of shelling out to curl.\n")
		b.WriteString("- Ask clarifying questions via `ask_user` when a critical detail is unclear.\n\n")
	}

	if len(opts.Skills) > 0 {
		b.WriteString("## Skills\n\n")
		b.WriteString("The following skill modules are available:\n\n")
		for _, s := range opts.Skills {
			desc := strings.TrimSpace(s.Description)
			if desc == "" {
				desc = "(no description)"
			}
			fmt.Fprintf(&b, "- **%s**: %s\n", s.Name, desc)
		}
		b.WriteString("\n")
	}

	if ctx := strings.TrimSpace(opts.ProjectContext); ctx != "" {
		b.WriteString("## Project context\n\n")
		b.WriteString(ctx)
		b.WriteString("\n")
	}

	return b.String()
}

// LoadProjectContext reads the first agent config file found in cwd
// (AGENTS.md, CLAUDE.md, or .agents/AGENTS.md). Missing files yield an
// empty string rather than an error so the builder can call it
// unconditionally.
func LoadProjectContext(cwd string) string {
	if cwd == "" {
		return ""
	}
	candidates := []string{
		filepath.Join(cwd, "AGENTS.md"),
		filepath.Join(cwd, "CLAUDE.md"),
		filepath.Join(cwd, ".agents", "AGENTS.md"),
		filepath.Join(cwd, ".gcode", "AGENTS.md"),
	}
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err == nil {
			return string(data)
		}
	}
	return ""
}
