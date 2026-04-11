package main

import (
	"strings"
	"testing"

	"github.com/gmoigneu/gcode/pkg/ai"
)

// TestMessageListRender verifies the component produces the expected
// layout for each role.
func TestMessageListRender(t *testing.T) {
	m := newMessageList()
	m.append(msgEntry{role: "user", text: "hello"})
	m.append(msgEntry{role: "tool", text: "[bash]"})

	lines := m.Render(80)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "hello") {
		t.Errorf("user text missing: %q", joined)
	}
	if !strings.Contains(joined, "bash") {
		t.Errorf("tool missing: %q", joined)
	}
}

func TestMessageListAssistantStreaming(t *testing.T) {
	m := newMessageList()
	m.appendAssistantDelta("hel")
	m.appendAssistantDelta("lo")

	lines := m.Render(80)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "hello") {
		t.Errorf("streaming text missing: %q", joined)
	}
}

func TestMessageListFinalizeAppendsFromStream(t *testing.T) {
	m := newMessageList()
	m.appendAssistantDelta("partial")
	m.finalizeAssistant(&ai.AssistantMessage{})
	if len(m.entries) != 1 || m.entries[0].role != "assistant" {
		t.Errorf("entries = %+v", m.entries)
	}
}

func TestMessageListFinalizeFallbackFromContent(t *testing.T) {
	m := newMessageList()
	asst := &ai.AssistantMessage{
		Content: []ai.Content{&ai.TextContent{Text: "final text"}},
	}
	m.finalizeAssistant(asst)
	if len(m.entries) != 1 || m.entries[0].text != "final text" {
		t.Errorf("entries = %+v", m.entries)
	}
}

func TestRenderEntryRoles(t *testing.T) {
	roles := []string{"user", "assistant", "tool", "tool-error", "error", "prompt"}
	for _, role := range roles {
		lines := renderEntry(msgEntry{role: role, text: "x"}, 80)
		if len(lines) == 0 {
			t.Errorf("role %q produced no lines", role)
		}
	}
}

func TestNewInteractiveAppConstructs(t *testing.T) {
	term := &stubTerminal{}
	app := newInteractiveApp(term, ai.Model{ID: "m"}, "", "/tmp", Args{})
	if app.root == nil || app.editor == nil || app.messages == nil {
		t.Fatal("components not constructed")
	}
}

// stubTerminal is a minimal TerminalIO for constructor tests that never
// enters the render loop.
type stubTerminal struct{}

func (s *stubTerminal) Write(data []byte) (int, error) { return len(data), nil }
func (s *stubTerminal) Width() int                     { return 80 }
func (s *stubTerminal) Height() int                    { return 24 }
func (s *stubTerminal) InputCh() <-chan []byte         { return nil }
func (s *stubTerminal) ResizeCh() <-chan struct{}      { return nil }
func (s *stubTerminal) Stop()                          {}
