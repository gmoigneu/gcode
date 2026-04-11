// Package tools implements the built-in tool set for the gcode agent:
// read, bash, edit, write, ask_user, and fetch.
package tools

import "github.com/gmoigneu/gcode/pkg/agent"

// CodingTools returns the default tool set, fully configured for a working
// directory and a question handler (used by ask_user).
func CodingTools(cwd string, questionHandler QuestionHandler) []agent.AgentTool {
	return []agent.AgentTool{
		*NewReadTool(cwd),
		*NewBashTool(cwd),
		*NewEditTool(cwd),
		*NewWriteTool(cwd),
		*NewAskUserTool(questionHandler),
		*NewFetchTool(),
	}
}
