// Package components contains the built-in TUI components (text, box,
// loader, status bar, select list, spacer, container).
package components

import (
	"strings"
	"sync"

	"github.com/gmoigneu/gcode/pkg/tui"
)

// Text is a simple wrapper that renders a static string. Long text is
// split on newlines; no word wrapping is performed here.
type Text struct {
	mu    sync.RWMutex
	text  string
	style func(string) string
}

// NewText constructs a Text component.
func NewText(text string) *Text { return &Text{text: text} }

// SetText replaces the text.
func (t *Text) SetText(s string) {
	t.mu.Lock()
	t.text = s
	t.mu.Unlock()
}

// SetStyle installs a styling function that wraps each rendered line.
func (t *Text) SetStyle(style func(string) string) {
	t.mu.Lock()
	t.style = style
	t.mu.Unlock()
}

// Render returns the text split into lines, optionally styled.
func (t *Text) Render(width int) []string {
	t.mu.RLock()
	text := t.text
	style := t.style
	t.mu.RUnlock()

	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	if style != nil {
		for i := range lines {
			lines[i] = style(lines[i])
		}
	}
	// Clip overly long lines so the TUI width guard is happy.
	for i, line := range lines {
		if tui.VisibleWidth(line) > width {
			lines[i] = tui.SliceByColumn(line, 0, width)
		}
	}
	return lines
}

// Invalidate is a no-op for Text because it maintains no cached state.
func (t *Text) Invalidate() {}
