package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gmoigneu/gcode/pkg/agent"
	"github.com/gmoigneu/gcode/pkg/ai"
)

// WriteParams is the schema of the write tool.
type WriteParams struct {
	Path    string `json:"path" description:"Path to the file to write"`
	Content string `json:"content" description:"Full content to write to the file"`
}

// NewWriteTool returns the write tool. Relative paths resolve against cwd,
// parent directories are created on demand, and concurrent writes to the
// same file serialise via the mutation queue.
func NewWriteTool(cwd string) *agent.AgentTool {
	return &agent.AgentTool{
		Tool: ai.Tool{
			Name:        "write",
			Description: "Write or overwrite a file. Creates parent directories automatically.",
			Parameters:  ai.SchemaFrom[WriteParams](),
		},
		Label: "write",
		Execute: func(id string, params map[string]any, signal context.Context, onUpdate agent.AgentToolUpdateFunc) (agent.AgentToolResult, error) {
			var p WriteParams
			if err := decodeParams(params, &p); err != nil {
				return agent.AgentToolResult{}, err
			}
			return executeWrite(cwd, p)
		},
	}
}

func executeWrite(cwd string, p WriteParams) (agent.AgentToolResult, error) {
	if p.Path == "" {
		return agent.AgentToolResult{}, errors.New("write: path is empty")
	}
	abs, err := ResolveToCwd(p.Path, cwd)
	if err != nil {
		return agent.AgentToolResult{}, err
	}

	var result agent.AgentToolResult

	err = WithFileMutationQueue(abs, func() error {
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return fmt.Errorf("write: mkdir: %w", err)
		}
		if err := os.WriteFile(abs, []byte(p.Content), 0o644); err != nil {
			return fmt.Errorf("write: %w", err)
		}
		msg := fmt.Sprintf("Wrote %d bytes (%s) to %s", len(p.Content), FormatSize(len(p.Content)), p.Path)
		result = agent.AgentToolResult{
			Content: []ai.Content{&ai.TextContent{Text: msg}},
			Details: map[string]any{
				"path":  abs,
				"bytes": len(p.Content),
			},
		}
		return nil
	})
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	return result, nil
}
