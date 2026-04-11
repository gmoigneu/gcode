package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/gmoigneu/gcode/internal/config"
	"github.com/gmoigneu/gcode/internal/prompt"
	"github.com/gmoigneu/gcode/pkg/agent"
	"github.com/gmoigneu/gcode/pkg/ai"
	"github.com/gmoigneu/gcode/pkg/tools"
	"github.com/gmoigneu/gcode/pkg/tui"
	"github.com/gmoigneu/gcode/pkg/tui/components"
)

// runInteractiveMode starts the full TUI: message list, status bar,
// and editor. Returns an exit code suitable for os.Exit.
func runInteractiveMode(args Args) int {
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

	term := tui.NewTerminal()
	if err := term.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "gcode: terminal start:", err)
		return 1
	}
	defer term.Stop()

	app := newInteractiveApp(term, model, apiKey, cwd, args)
	if err := app.start(); err != nil {
		fmt.Fprintln(os.Stderr, "gcode:", err)
		return 1
	}
	<-app.done
	return 0
}

// interactiveApp owns every runtime piece of the TUI mode.
type interactiveApp struct {
	ui       *tui.TUI
	term     tui.TerminalIO
	root     *components.Container
	messages *messageList
	status   *components.StatusBar
	loader   *components.Loader
	editor   *components.Editor

	agent   *agent.Agent
	model   ai.Model
	apiKey  string
	cwd     string
	args    Args
	done    chan struct{}
	closeMu sync.Mutex
	closed  bool
}

// AskUser implements tools.QuestionHandler.
func (a *interactiveApp) AskUser(p tools.AskUserParams) (tools.AskUserResult, error) {
	// Minimal implementation: display the question in the message list
	// and accept the next editor submit as the freeform answer. A full
	// implementation would show a select list overlay; see spec §7.3.
	a.messages.append(msgEntry{role: "prompt", text: p.Question})
	a.ui.Render()

	reply := make(chan string, 1)
	var once sync.Once
	a.editor.OnSubmit = func(text string) {
		once.Do(func() {
			reply <- strings.TrimSpace(text)
			a.editor.SetText("")
			a.editor.OnSubmit = a.handleEditorSubmit
		})
	}
	answer := <-reply
	if answer == "" {
		return tools.AskUserResult{Cancelled: true}, nil
	}
	return tools.AskUserResult{Freeform: answer}, nil
}

// newInteractiveApp builds a fresh interactive application tied to the
// provided terminal.
func newInteractiveApp(term tui.TerminalIO, model ai.Model, apiKey, cwd string, args Args) *interactiveApp {
	app := &interactiveApp{
		term:   term,
		model:  model,
		apiKey: apiKey,
		cwd:    cwd,
		args:   args,
		done:   make(chan struct{}),
	}

	app.messages = newMessageList()
	app.status = components.NewStatusBar()
	app.loader = components.NewLoader()
	app.editor = components.NewEditor(tui.NewKeybindingsManager())
	app.editor.OnSubmit = app.handleEditorSubmit

	app.root = components.NewContainer()
	app.root.AddChild(app.messages)
	app.root.AddChild(app.loader)
	app.root.AddChild(app.status)
	app.root.AddChild(components.NewSpacer(1))
	app.root.AddChild(components.NewText("❯ "))
	app.root.AddChild(app.editor)

	app.ui = tui.New(term, app.root)
	app.ui.SetFocus(app.editor)
	return app
}

// start wires the agent and begins the render loop.
func (a *interactiveApp) start() error {
	toolset := tools.CodingTools(a.cwd, a)
	discovery := config.RunDiscovery(config.DefaultDiscoveryPaths(a.cwd))
	systemPrompt := a.args.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = prompt.BuildSystemPrompt(prompt.Options{
			Cwd:            a.cwd,
			Tools:          toolset,
			ProjectContext: discovery.ProjectContext,
			Skills:         skillSummaries(discovery.Skills),
		})
	}

	a.agent = agent.New(agent.AgentConfig{
		GetAPIKey: func(_ ai.Provider) string { return a.apiKey },
	})
	a.agent.SetSystemPrompt(systemPrompt)
	a.agent.SetTools(toolset)
	a.agent.SetModel(a.model, ai.ThinkingLevel(a.args.ThinkingLevel))
	a.agent.Subscribe(a.handleAgentEvent)

	a.status.SetModel(fmt.Sprintf("%s/%s", a.model.Provider, a.model.ID), a.args.ThinkingLevel)
	a.ui.AddInputListener(a.globalInput)

	a.ui.Start()

	if a.args.Prompt != "" {
		go a.runPrompt(a.args.Prompt)
	}
	return nil
}

// handleEditorSubmit is the default editor submit callback: hand the
// text to the agent.
func (a *interactiveApp) handleEditorSubmit(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if text == "/quit" || text == "/exit" {
		a.quit()
		return
	}
	a.editor.SetText("")
	go a.runPrompt(text)
}

// runPrompt forwards a user prompt to the agent and waits for
// completion.
func (a *interactiveApp) runPrompt(text string) {
	a.messages.append(msgEntry{role: "user", text: text})
	a.ui.Render()
	if err := a.agent.Run(text); err != nil {
		a.messages.append(msgEntry{role: "error", text: err.Error()})
		a.ui.Render()
	}
}

// handleAgentEvent forwards agent events into the UI.
func (a *interactiveApp) handleAgentEvent(e agent.AgentEvent, _ context.Context) {
	switch e.Type {
	case agent.MessageUpdate:
		if e.AssistantMessageEvent != nil && e.AssistantMessageEvent.Type == ai.EventTextDelta {
			a.messages.appendAssistantDelta(e.AssistantMessageEvent.Delta)
		}
	case agent.MessageEnd:
		if asst, ok := e.Message.(*ai.AssistantMessage); ok {
			a.messages.finalizeAssistant(asst)
			a.status.SetUsage(asst.Usage.TotalTokens, asst.Usage.Cost.Total)
		}
	case agent.ToolExecutionStart:
		a.messages.append(msgEntry{role: "tool", text: fmt.Sprintf("[%s]", e.ToolName)})
	case agent.ToolExecutionEnd:
		if e.ToolIsError {
			a.messages.append(msgEntry{role: "tool-error", text: fmt.Sprintf("[%s: error]", e.ToolName)})
		}
	case agent.LivenessUpdate:
		if e.Liveness != nil {
			a.loader.SetLiveness(*e.Liveness)
			a.status.SetLiveness(*e.Liveness)
		}
	}
	a.ui.Render()
}

// globalInput is the TUI input listener. It handles ctrl+c (abort /
// quit) before the editor sees it.
func (a *interactiveApp) globalInput(data []byte) (bool, []byte) {
	if !tui.MatchesKey(data, "ctrl+c") {
		return false, nil
	}
	state := a.agent.State()
	if state != nil && state.IsStreaming {
		a.agent.Abort()
		return true, nil
	}
	a.quit()
	return true, nil
}

// quit asks the TUI loop to stop and closes the done channel once.
func (a *interactiveApp) quit() {
	a.closeMu.Lock()
	defer a.closeMu.Unlock()
	if a.closed {
		return
	}
	a.closed = true
	a.ui.Stop()
	close(a.done)
}

// messageList is a custom component that accumulates chat history.
type messageList struct {
	mu        sync.RWMutex
	entries   []msgEntry
	streaming strings.Builder
	hasStream bool
}

type msgEntry struct {
	role string
	text string
}

func newMessageList() *messageList { return &messageList{} }

func (m *messageList) append(e msgEntry) {
	m.flushStream()
	m.mu.Lock()
	m.entries = append(m.entries, e)
	m.mu.Unlock()
}

func (m *messageList) appendAssistantDelta(delta string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hasStream = true
	m.streaming.WriteString(delta)
}

func (m *messageList) finalizeAssistant(asst *ai.AssistantMessage) {
	m.mu.Lock()
	text := m.streaming.String()
	m.streaming.Reset()
	m.hasStream = false
	if text == "" {
		// The provider delivered the whole thing in the final message.
		for _, c := range asst.Content {
			if tc, ok := c.(*ai.TextContent); ok {
				text += tc.Text
			}
		}
	}
	m.entries = append(m.entries, msgEntry{role: "assistant", text: text})
	m.mu.Unlock()
}

func (m *messageList) flushStream() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.hasStream {
		return
	}
	text := m.streaming.String()
	m.streaming.Reset()
	m.hasStream = false
	m.entries = append(m.entries, msgEntry{role: "assistant", text: text})
}

// Render paints the accumulated history. The current streaming buffer
// (if any) is appended as a pseudo-entry so the user sees tokens as
// they arrive.
func (m *messageList) Render(width int) []string {
	m.mu.RLock()
	entries := append([]msgEntry(nil), m.entries...)
	streaming := m.streaming.String()
	hasStream := m.hasStream
	m.mu.RUnlock()

	var lines []string
	for _, e := range entries {
		lines = append(lines, renderEntry(e, width)...)
		lines = append(lines, "")
	}
	if hasStream {
		lines = append(lines, renderEntry(msgEntry{role: "assistant", text: streaming}, width)...)
	}
	return lines
}

func (m *messageList) Invalidate() {}

func renderEntry(e msgEntry, width int) []string {
	switch e.role {
	case "user":
		return []string{"\x1b[1;36m▸ " + e.text + "\x1b[0m"}
	case "prompt":
		return []string{"\x1b[1;35m? " + e.text + "\x1b[0m"}
	case "tool":
		return []string{"\x1b[33m" + e.text + "\x1b[0m"}
	case "tool-error":
		return []string{"\x1b[31m" + e.text + "\x1b[0m"}
	case "error":
		return []string{"\x1b[31mError: " + e.text + "\x1b[0m"}
	case "assistant":
		md := components.NewMarkdown(e.text)
		return md.Render(width)
	}
	return []string{e.text}
}
