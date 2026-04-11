package tui

import (
	"strings"
	"testing"
)

// staticComponent is a minimal Component used to satisfy the interface
// in tests.
type staticComponent struct {
	lines    []string
	focused  bool
	renderN  int
	invalidN int
}

func (s *staticComponent) Render(width int) []string {
	s.renderN++
	return s.lines
}

func (s *staticComponent) Invalidate()             { s.invalidN++ }
func (s *staticComponent) HandleInput(data []byte) {}
func (s *staticComponent) SetFocused(f bool)       { s.focused = f }
func (s *staticComponent) IsFocused() bool         { return s.focused }

func TestComponentInterfaceSatisfaction(t *testing.T) {
	var _ Component = (*staticComponent)(nil)
	var _ InputHandler = (*staticComponent)(nil)
	var _ Focusable = (*staticComponent)(nil)
}

func TestRenderInvokesComponent(t *testing.T) {
	c := &staticComponent{lines: []string{"hello", "world"}}
	got := c.Render(20)
	if len(got) != 2 {
		t.Errorf("got %d lines", len(got))
	}
	if c.renderN != 1 {
		t.Errorf("render not called")
	}
}

func TestInvalidate(t *testing.T) {
	c := &staticComponent{}
	c.Invalidate()
	if c.invalidN != 1 {
		t.Errorf("invalidate count = %d", c.invalidN)
	}
}

func TestFocusable(t *testing.T) {
	c := &staticComponent{}
	if c.IsFocused() {
		t.Error("default should be unfocused")
	}
	c.SetFocused(true)
	if !c.IsFocused() {
		t.Error("should be focused after SetFocused(true)")
	}
}

func TestCursorMarkerUniqueness(t *testing.T) {
	if CursorMarker == "" {
		t.Fatal("CursorMarker should not be empty")
	}
	if !strings.HasPrefix(CursorMarker, "\x1b_") {
		t.Errorf("CursorMarker should be an APC sequence, got %q", CursorMarker)
	}
}

func TestResetConstant(t *testing.T) {
	if Reset != "\x1b[0m" {
		t.Errorf("Reset = %q", Reset)
	}
}
