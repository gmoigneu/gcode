package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gmoigneu/gcode/pkg/agent"
	"github.com/gmoigneu/gcode/pkg/ai"
)

// ReadParams is the JSON schema of the read tool.
type ReadParams struct {
	Path   string `json:"path" description:"Path to the file to read (relative or absolute)"`
	Offset *int   `json:"offset,omitempty" description:"1-indexed line number to start reading from"`
	Limit  *int   `json:"limit,omitempty" description:"Maximum number of lines to read"`
}

// NewReadTool returns the read tool with paths resolved relative to cwd.
func NewReadTool(cwd string) *agent.AgentTool {
	return &agent.AgentTool{
		Tool: ai.Tool{
			Name:        "read",
			Description: "Read the contents of a file. Supports text and image files.",
			Parameters:  ai.SchemaFrom[ReadParams](),
		},
		Label: "read",
		Execute: func(id string, params map[string]any, signal context.Context, onUpdate agent.AgentToolUpdateFunc) (agent.AgentToolResult, error) {
			var p ReadParams
			if err := decodeParams(params, &p); err != nil {
				return agent.AgentToolResult{}, err
			}
			return executeRead(cwd, p)
		},
	}
}

// executeRead is the bulk of NewReadTool — split out so tests can reach it
// directly without going through the agent shell.
func executeRead(cwd string, p ReadParams) (agent.AgentToolResult, error) {
	abs, err := ResolveToCwd(p.Path, cwd)
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return agent.AgentToolResult{}, fmt.Errorf("read: %w", err)
	}
	if info.IsDir() {
		return agent.AgentToolResult{}, fmt.Errorf("read: %q is a directory", p.Path)
	}

	if isImagePath(abs) {
		return readImage(abs, info.Size())
	}

	return readText(abs, p)
}

func isImagePath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp":
		return true
	}
	return false
}

func readImage(path string, size int64) (agent.AgentToolResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return agent.AgentToolResult{}, fmt.Errorf("read: %w", err)
	}
	mime := mimeForExt(filepath.Ext(path))
	encoded := base64.StdEncoding.EncodeToString(data)
	return agent.AgentToolResult{
		Content: []ai.Content{
			&ai.TextContent{Text: fmt.Sprintf("[Image attached: %s, %s]", filepath.Base(path), FormatSize(int(size)))},
			&ai.ImageContent{Data: encoded, MimeType: mime},
		},
	}, nil
}

func mimeForExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	}
	return "application/octet-stream"
}

func readText(abs string, p ReadParams) (agent.AgentToolResult, error) {
	data, err := os.ReadFile(abs)
	if err != nil {
		return agent.AgentToolResult{}, fmt.Errorf("read: %w", err)
	}
	content := string(data)
	lines := strings.Split(content, "\n")
	totalLines := len(lines)

	startIdx := 0
	if p.Offset != nil {
		if *p.Offset < 1 {
			return agent.AgentToolResult{}, fmt.Errorf("read: offset must be >= 1")
		}
		startIdx = *p.Offset - 1
		if startIdx >= totalLines {
			return agent.AgentToolResult{}, fmt.Errorf("read: offset %d exceeds file length (%d lines)", *p.Offset, totalLines)
		}
	}

	userLimit := -1
	if p.Limit != nil {
		if *p.Limit <= 0 {
			return agent.AgentToolResult{}, fmt.Errorf("read: limit must be > 0")
		}
		userLimit = *p.Limit
		endIdx := startIdx + userLimit
		if endIdx > totalLines {
			endIdx = totalLines
		}
		lines = lines[startIdx:endIdx]
	} else {
		lines = lines[startIdx:]
	}
	slice := strings.Join(lines, "\n")

	tr := TruncateHead(slice, nil)

	if tr.FirstLineExceedsLimit {
		msg := fmt.Sprintf("First line of %q exceeds the %d-byte limit. Use bash with `sed -n '%dp' '%s' | head -c %d` to read it in chunks.",
			p.Path, tr.MaxBytes, startIdx+1, abs, tr.MaxBytes)
		return agent.AgentToolResult{
			Content: []ai.Content{&ai.TextContent{Text: msg}},
		}, nil
	}

	body := tr.Content
	var suffix string

	shownFrom := startIdx + 1
	shownTo := startIdx + tr.OutputLines

	switch {
	case tr.Truncated:
		next := shownTo + 1
		suffix = fmt.Sprintf("\n\n[Showing lines %d-%d of %d. Use offset=%d to continue.]",
			shownFrom, shownTo, totalLines, next)
	case userLimit > 0 && shownTo < totalLines:
		next := shownTo + 1
		remaining := totalLines - shownTo
		suffix = fmt.Sprintf("\n\n[%d more lines in file. Use offset=%d to continue.]",
			remaining, next)
	}

	return agent.AgentToolResult{
		Content: []ai.Content{&ai.TextContent{Text: body + suffix}},
		Details: map[string]any{
			"path":         abs,
			"total_lines":  totalLines,
			"output_lines": tr.OutputLines,
			"offset":       startIdx + 1,
		},
	}, nil
}

// decodeParams marshals/unmarshals via JSON so we can reuse the struct tag
// shape that matches the tool's JSON Schema.
func decodeParams(params map[string]any, out any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("params: marshal: %w", err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("params: unmarshal: %w", err)
	}
	return nil
}
