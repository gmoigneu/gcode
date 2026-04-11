package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/gmoigneu/gcode/pkg/agent"
	"github.com/gmoigneu/gcode/pkg/ai"
)

// EditParams is the schema of the edit tool.
type EditParams struct {
	Path  string     `json:"path" description:"Path to the file to edit"`
	Edits []EditPair `json:"edits" description:"One or more targeted oldText/newText replacements"`
}

// NewEditTool returns the edit tool. It resolves paths relative to cwd,
// preserves BOM and line endings, and applies multiple edits atomically.
func NewEditTool(cwd string) *agent.AgentTool {
	return &agent.AgentTool{
		Tool: ai.Tool{
			Name:        "edit",
			Description: "Apply one or more search-and-replace edits to a file. Preserves line endings and BOM.",
			Parameters:  ai.SchemaFrom[EditParams](),
		},
		Label:            "edit",
		PrepareArguments: prepareEditArguments,
		Execute: func(id string, params map[string]any, signal context.Context, onUpdate agent.AgentToolUpdateFunc) (agent.AgentToolResult, error) {
			var p EditParams
			if err := decodeParams(params, &p); err != nil {
				return agent.AgentToolResult{}, err
			}
			return executeEdit(cwd, p)
		},
	}
}

// prepareEditArguments migrates the legacy {oldText, newText} shape into
// the {edits: [{oldText, newText}]} shape the tool actually accepts.
func prepareEditArguments(args map[string]any) map[string]any {
	if args == nil {
		return args
	}
	if _, ok := args["edits"]; ok {
		return args
	}
	oldText, okOld := args["oldText"].(string)
	newText, _ := args["newText"].(string)
	if !okOld {
		return args
	}
	args["edits"] = []any{
		map[string]any{"oldText": oldText, "newText": newText},
	}
	delete(args, "oldText")
	delete(args, "newText")
	return args
}

func executeEdit(cwd string, p EditParams) (agent.AgentToolResult, error) {
	if len(p.Edits) == 0 {
		return agent.AgentToolResult{}, errors.New("edit: edits array is empty")
	}
	abs, err := ResolveToCwd(p.Path, cwd)
	if err != nil {
		return agent.AgentToolResult{}, err
	}

	var result agent.AgentToolResult

	err = WithFileMutationQueue(abs, func() error {
		info, err := os.Stat(abs)
		if err != nil {
			return fmt.Errorf("edit: %w", err)
		}
		if info.IsDir() {
			return fmt.Errorf("edit: %q is a directory", p.Path)
		}

		data, err := os.ReadFile(abs)
		if err != nil {
			return fmt.Errorf("edit: read: %w", err)
		}
		rawContent := string(data)

		bom := StripBom(rawContent)
		ending := DetectLineEnding(bom.Text)
		normalized := NormalizeToLF(bom.Text)

		editResult, err := ApplyEdits(normalized, p.Edits, p.Path)
		if err != nil {
			return err
		}

		restored := RestoreLineEndings(editResult.NewContent, ending)
		final := bom.Bom + restored

		if err := os.WriteFile(abs, []byte(final), info.Mode().Perm()); err != nil {
			return fmt.Errorf("edit: write: %w", err)
		}

		diff := GenerateDiff(editResult.BaseContent, editResult.NewContent, 4)
		summary := fmt.Sprintf("Applied %d edit", len(p.Edits))
		if len(p.Edits) != 1 {
			summary += "s"
		}
		summary += fmt.Sprintf(" to %s", p.Path)
		if diff.FirstChangedLine != nil {
			summary += fmt.Sprintf(" (first change at line %d)", *diff.FirstChangedLine)
		}

		body := summary
		if diff.Diff != "" {
			body += "\n\n" + diff.Diff
		}

		details := map[string]any{
			"path":      abs,
			"editCount": len(p.Edits),
			"diff":      diff.Diff,
		}
		if diff.FirstChangedLine != nil {
			details["firstChangedLine"] = *diff.FirstChangedLine
		}

		result = agent.AgentToolResult{
			Content: []ai.Content{&ai.TextContent{Text: body}},
			Details: details,
		}
		return nil
	})
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	return result, nil
}

// ensure json package is referenced (PrepareArguments reuses encoding types).
var _ = json.Marshal
