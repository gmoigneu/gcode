package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gmoigneu/gcode/internal/config"
	"github.com/gmoigneu/gcode/internal/prompt"
	"github.com/gmoigneu/gcode/pkg/agent"
	"github.com/gmoigneu/gcode/pkg/ai"
	"github.com/gmoigneu/gcode/pkg/plugin"
	"github.com/gmoigneu/gcode/pkg/tools"
)

// runPrintMode is the non-interactive pipe mode: no TUI, one prompt,
// stream the assistant text to stdout, report tool execution on
// stderr, exit when the agent completes.
func runPrintMode(args Args) int {
	model, err := ResolveModel(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gcode:", err)
		return 2
	}
	auth := config.LoadAuth(config.DefaultAuthPath())
	apiKey := ResolveAPIKey(args, model, auth)

	cwd := args.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	toolset := tools.CodingTools(cwd, nil) // nil handler disables ask_user

	discovery := config.RunDiscovery(config.DefaultDiscoveryPaths(cwd))
	systemPrompt := args.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = prompt.BuildSystemPrompt(prompt.Options{
			Cwd:            cwd,
			Tools:          toolset,
			ProjectContext: discovery.ProjectContext,
			Skills:         skillSummaries(discovery.Skills),
		})
	}

	a := agent.New(agent.AgentConfig{
		GetAPIKey: func(_ ai.Provider) string { return apiKey },
	})
	a.SetSystemPrompt(systemPrompt)
	a.SetTools(toolset)
	thinking := ai.ThinkingLevel(args.ThinkingLevel)
	a.SetModel(model, thinking)

	// Stream text to stdout and tool events to stderr.
	a.Subscribe(func(e agent.AgentEvent, _ context.Context) {
		switch e.Type {
		case agent.MessageUpdate:
			if e.AssistantMessageEvent != nil && e.AssistantMessageEvent.Type == ai.EventTextDelta {
				fmt.Print(e.AssistantMessageEvent.Delta)
			}
		case agent.ToolExecutionStart:
			fmt.Fprintf(os.Stderr, "\n[tool: %s]\n", e.ToolName)
		case agent.ToolExecutionEnd:
			if e.ToolIsError {
				fmt.Fprintf(os.Stderr, "[tool error]\n")
			}
		case agent.MessageEnd:
			if asst, ok := e.Message.(*ai.AssistantMessage); ok {
				if asst.ErrorMessage != "" {
					fmt.Fprintf(os.Stderr, "\ngcode: %s\n", asst.ErrorMessage)
				}
			}
		}
	})

	promptText := strings.TrimSpace(args.Prompt)
	if promptText == "" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, "gcode: read stdin:", err)
			return 2
		}
		promptText = strings.TrimSpace(string(data))
	}
	if promptText == "" {
		fmt.Fprintln(os.Stderr, "gcode: empty prompt (use --prompt or pipe via stdin)")
		return 2
	}

	if err := a.Run(promptText); err != nil {
		fmt.Fprintf(os.Stderr, "\ngcode: %s\n", err)
		return 1
	}
	fmt.Println()
	return 0
}

// skillSummaries converts plugin.Skill into the lightweight
// SkillSummary used by the prompt builder.
func skillSummaries(skills []plugin.Skill) []prompt.SkillSummary {
	out := make([]prompt.SkillSummary, 0, len(skills))
	for _, s := range skills {
		out = append(out, prompt.SkillSummary{Name: s.Name, Description: s.Description})
	}
	return out
}
