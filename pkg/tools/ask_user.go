package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/gmoigneu/gcode/pkg/agent"
	"github.com/gmoigneu/gcode/pkg/ai"
)

// AskOption is a single choice offered to the user.
type AskOption struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

// AskUserParams is the schema of the ask_user tool.
type AskUserParams struct {
	Question      string      `json:"question" description:"The question to ask the user"`
	Options       []AskOption `json:"options,omitempty" description:"Optional list of choices"`
	AllowFreeform *bool       `json:"allowFreeform,omitempty" description:"Allow a freeform text answer. Default: true"`
	AllowMultiple *bool       `json:"allowMultiple,omitempty" description:"Allow selecting multiple options. Default: false"`
	AllowComment  *bool       `json:"allowComment,omitempty" description:"Collect an optional comment. Default: false"`
	Context       string      `json:"context,omitempty" description:"Relevant context to show before the question"`
}

// AskUserResult is returned to the tool by the TUI.
type AskUserResult struct {
	Selected  []string `json:"selected,omitempty"`
	Freeform  string   `json:"freeform,omitempty"`
	Comment   string   `json:"comment,omitempty"`
	Cancelled bool     `json:"cancelled"`
}

// QuestionHandler is implemented by the TUI. It blocks until the user
// responds and returns the structured result.
type QuestionHandler interface {
	AskUser(params AskUserParams) (AskUserResult, error)
}

// QuestionHandlerFunc is a functional shortcut implementing QuestionHandler.
type QuestionHandlerFunc func(params AskUserParams) (AskUserResult, error)

// AskUser invokes the function.
func (f QuestionHandlerFunc) AskUser(p AskUserParams) (AskUserResult, error) {
	return f(p)
}

// NewAskUserTool returns the ask_user tool bound to a QuestionHandler.
func NewAskUserTool(handler QuestionHandler) *agent.AgentTool {
	return &agent.AgentTool{
		Tool: ai.Tool{
			Name:        "ask_user",
			Description: "Ask the user a question with optional multiple-choice answers. Blocks until the user responds.",
			Parameters:  ai.SchemaFrom[AskUserParams](),
		},
		Label: "ask_user",
		Execute: func(id string, params map[string]any, signal context.Context, onUpdate agent.AgentToolUpdateFunc) (agent.AgentToolResult, error) {
			var p AskUserParams
			if err := decodeParams(params, &p); err != nil {
				return agent.AgentToolResult{}, err
			}
			return executeAskUser(signal, handler, p)
		},
	}
}

func executeAskUser(ctx context.Context, handler QuestionHandler, p AskUserParams) (agent.AgentToolResult, error) {
	if handler == nil {
		return agent.AgentToolResult{}, errors.New("ask_user: no question handler registered")
	}
	if p.Question == "" {
		return agent.AgentToolResult{}, errors.New("ask_user: question is empty")
	}

	// Default AllowFreeform to true when unspecified.
	if p.AllowFreeform == nil {
		t := true
		p.AllowFreeform = &t
	}

	// Handler call may block. Honour context cancellation by returning a
	// cancelled result if the ctx fires before the handler does.
	type handlerOutput struct {
		result AskUserResult
		err    error
	}
	out := make(chan handlerOutput, 1)
	go func() {
		r, err := handler.AskUser(p)
		out <- handlerOutput{result: r, err: err}
	}()

	select {
	case ho := <-out:
		if ho.err != nil {
			return agent.AgentToolResult{}, fmt.Errorf("ask_user: %w", ho.err)
		}
		return formatAskResult(ho.result), nil
	case <-ctx.Done():
		return formatAskResult(AskUserResult{Cancelled: true}), nil
	}
}

func formatAskResult(r AskUserResult) agent.AgentToolResult {
	if r.Cancelled {
		return agent.AgentToolResult{
			Content: []ai.Content{&ai.TextContent{Text: "[User cancelled the question.]"}},
			Details: map[string]any{"cancelled": true},
		}
	}

	var parts []string
	if len(r.Selected) > 0 {
		parts = append(parts, "Selected: "+strings.Join(r.Selected, ", "))
	}
	if r.Freeform != "" {
		parts = append(parts, "Response: "+r.Freeform)
	}
	if r.Comment != "" {
		parts = append(parts, "Comment: "+r.Comment)
	}
	text := strings.Join(parts, "\n")
	if text == "" {
		text = "[User responded with no content.]"
	}
	return agent.AgentToolResult{
		Content: []ai.Content{&ai.TextContent{Text: text}},
		Details: map[string]any{
			"selected":  r.Selected,
			"freeform":  r.Freeform,
			"comment":   r.Comment,
			"cancelled": false,
		},
	}
}
