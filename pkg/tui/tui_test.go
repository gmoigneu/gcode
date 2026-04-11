package tui

import (
	"strings"
	"testing"
	"time"
)

// ----- helpers -----

type fixedComponent struct {
	lines  []string
	render int
}

func (f *fixedComponent) Render(width int) []string { f.render++; return f.lines }
func (f *fixedComponent) Invalidate()               {}

type inputTracker struct {
	fixedComponent
	focused bool
	inputs  [][]byte
}

func (i *inputTracker) HandleInput(data []byte) {
	cp := make([]byte, len(data))
	copy(cp, data)
	i.inputs = append(i.inputs, cp)
}
func (i *inputTracker) SetFocused(f bool) { i.focused = f }
func (i *inputTracker) IsFocused() bool   { return i.focused }

// ----- basic rendering -----

func TestTUIInitialRender(t *testing.T) {
	vt := NewVirtualTerminal(20, 5)
	root := &fixedComponent{lines: []string{"line1", "line2"}}
	tui := New(vt, root)

	tui.ForceRender()

	out := vt.FullOutput()
	if !strings.Contains(out, "line1") || !strings.Contains(out, "line2") {
		t.Errorf("missing content: %q", out)
	}
	if !strings.Contains(out, "\x1b[2J") {
		t.Errorf("full redraw should clear screen: %q", out)
	}
}

func TestTUIDiffRenderOnlyChanged(t *testing.T) {
	vt := NewVirtualTerminal(20, 5)
	root := &fixedComponent{lines: []string{"a", "b", "c"}}
	tui := New(vt, root)
	tui.minRenderInterval = 0

	tui.ForceRender()
	vt.Reset()

	root.lines = []string{"a", "B", "c"} // only middle line changed
	tui.Render()
	out := vt.FullOutput()
	if !strings.Contains(out, "B") {
		t.Errorf("change not emitted: %q", out)
	}
}

func TestTUIRenderRateLimit(t *testing.T) {
	vt := NewVirtualTerminal(20, 5)
	root := &fixedComponent{lines: []string{"x"}}
	tui := New(vt, root)
	tui.minRenderInterval = 100 * time.Millisecond

	tui.ForceRender()
	vt.Reset()
	tui.Render() // rate-limited, should be dropped
	if len(vt.Writes()) != 0 {
		t.Errorf("rate limit violated: %d writes", len(vt.Writes()))
	}
}

func TestTUIResizeForcesFullRedraw(t *testing.T) {
	vt := NewVirtualTerminal(20, 5)
	root := &fixedComponent{lines: []string{"hello"}}
	tui := New(vt, root)
	tui.minRenderInterval = 0

	tui.ForceRender()
	vt.Reset()

	vt.Resize(40, 10)
	tui.ForceRender()
	out := vt.FullOutput()
	if !strings.Contains(out, "\x1b[2J") {
		t.Errorf("resize should clear screen: %q", out)
	}
}

// ----- cursor marker -----

func TestTUICursorMarkerPositionsCursor(t *testing.T) {
	vt := NewVirtualTerminal(20, 5)
	root := &fixedComponent{lines: []string{"abc" + CursorMarker + "def"}}
	tui := New(vt, root)

	tui.ForceRender()
	out := vt.FullOutput()
	// Should contain a cursor positioning escape (1-indexed row 1, col 4).
	if !strings.Contains(out, "\x1b[1;4H") {
		t.Errorf("cursor not positioned: %q", out)
	}
	// The CursorMarker itself must be stripped.
	if strings.Contains(out, CursorMarker) {
		t.Errorf("cursor marker leaked: %q", out)
	}
	// Cursor should be visible.
	if !strings.Contains(out, "\x1b[?25h") {
		t.Errorf("cursor show missing: %q", out)
	}
}

func TestTUINoCursorHidesHardwareCursor(t *testing.T) {
	vt := NewVirtualTerminal(20, 5)
	root := &fixedComponent{lines: []string{"no cursor"}}
	tui := New(vt, root)

	tui.ForceRender()
	out := vt.FullOutput()
	if !strings.Contains(out, "\x1b[?25l") {
		t.Errorf("cursor should be hidden: %q", out)
	}
}

// ----- width overflow clipping -----

func TestTUIClipsOverwideLines(t *testing.T) {
	vt := NewVirtualTerminal(10, 5)
	root := &fixedComponent{lines: []string{strings.Repeat("x", 50)}}
	tui := New(vt, root)
	tui.ForceRender()
	// The TUI should have clipped the line to 10 visible columns.
	if VisibleWidth(tui.previousLines[0]) > 10 {
		t.Errorf("not clipped: %d", VisibleWidth(tui.previousLines[0]))
	}
}

// ----- overlays -----

func TestTUIOverlayOverlaysBase(t *testing.T) {
	vt := NewVirtualTerminal(20, 5)
	root := &fixedComponent{lines: []string{"base content here"}}
	tui := New(vt, root)

	overlay := &fixedComponent{lines: []string{"OV"}}
	tui.ShowOverlay(overlay, OverlayOptions{Row: 0, Col: 0, Width: 2})
	tui.ForceRender()

	if !strings.Contains(tui.previousLines[0], "OV") {
		t.Errorf("overlay missing: %q", tui.previousLines[0])
	}
}

func TestTUIOverlayHideShowClose(t *testing.T) {
	vt := NewVirtualTerminal(20, 5)
	root := &fixedComponent{lines: []string{"base"}}
	tui := New(vt, root)
	tui.minRenderInterval = 0

	overlay := &fixedComponent{lines: []string{"OV"}}
	handle := tui.ShowOverlay(overlay, OverlayOptions{Row: 0, Col: 0, Width: 2})
	tui.ForceRender()
	if !strings.Contains(tui.previousLines[0], "OV") {
		t.Error("overlay should be visible")
	}

	handle.Hide()
	tui.ForceRender()
	if strings.Contains(tui.previousLines[0], "OV") {
		t.Error("overlay should be hidden")
	}

	handle.Show()
	tui.ForceRender()
	if !strings.Contains(tui.previousLines[0], "OV") {
		t.Error("overlay should be visible again")
	}

	handle.Close()
	tui.ForceRender()
	if strings.Contains(tui.previousLines[0], "OV") {
		t.Error("overlay should be removed")
	}
}

// ----- input routing -----

func TestTUIInputRoutesToFocused(t *testing.T) {
	vt := NewVirtualTerminal(20, 5)
	input := &inputTracker{fixedComponent: fixedComponent{lines: []string{"x"}}}
	tui := New(vt, input)
	tui.SetFocus(input)
	tui.minRenderInterval = 0

	tui.handleInput([]byte("a"))
	if len(input.inputs) != 1 || string(input.inputs[0]) != "a" {
		t.Errorf("inputs = %v", input.inputs)
	}
	if !input.focused {
		t.Error("focused flag not set")
	}
}

func TestTUIInputListenerConsume(t *testing.T) {
	vt := NewVirtualTerminal(20, 5)
	input := &inputTracker{fixedComponent: fixedComponent{lines: []string{"x"}}}
	tui := New(vt, input)
	tui.SetFocus(input)
	tui.AddInputListener(func(data []byte) (bool, []byte) {
		return true, nil
	})
	tui.handleInput([]byte("a"))
	if len(input.inputs) != 0 {
		t.Error("consumed input should not reach component")
	}
}

func TestTUIInputListenerRewrite(t *testing.T) {
	vt := NewVirtualTerminal(20, 5)
	input := &inputTracker{fixedComponent: fixedComponent{lines: []string{"x"}}}
	tui := New(vt, input)
	tui.SetFocus(input)
	tui.AddInputListener(func(data []byte) (bool, []byte) {
		return false, []byte("Z")
	})
	tui.handleInput([]byte("a"))
	if len(input.inputs) != 1 || string(input.inputs[0]) != "Z" {
		t.Errorf("rewrite failed: %v", input.inputs)
	}
}

func TestTUIInputFiltersKeyRelease(t *testing.T) {
	vt := NewVirtualTerminal(20, 5)
	input := &inputTracker{fixedComponent: fixedComponent{lines: []string{"x"}}}
	tui := New(vt, input)
	tui.SetFocus(input)

	release := []byte("\x1b[97;1:3u")
	tui.handleInput(release)
	if len(input.inputs) != 0 {
		t.Error("key release should be filtered")
	}
}

// ----- stop -----

func TestTUIStopSafeMultipleTimes(t *testing.T) {
	vt := NewVirtualTerminal(20, 5)
	root := &fixedComponent{lines: []string{"x"}}
	tui := New(vt, root)
	tui.Stop()
	tui.Stop()
}

// ----- composite helpers -----

func TestCompositeLineAt(t *testing.T) {
	base := "012345678901234567890"
	overlay := "###"
	got := compositeLineAt(base, overlay, 5, 21)
	if !strings.Contains(got, "###") {
		t.Errorf("got %q", got)
	}
	// Width preserved.
	if VisibleWidth(got) != VisibleWidth(base) {
		t.Errorf("width changed: %d vs %d", VisibleWidth(got), VisibleWidth(base))
	}
}

func TestExtractCursor(t *testing.T) {
	lines := []string{
		"first",
		"second" + CursorMarker,
	}
	row, col := extractCursor(lines)
	if row != 1 || col != 6 {
		t.Errorf("row=%d col=%d", row, col)
	}
}

func TestExtractCursorAbsent(t *testing.T) {
	lines := []string{"no cursor here"}
	row, col := extractCursor(lines)
	if row != -1 || col != -1 {
		t.Errorf("row=%d col=%d", row, col)
	}
}

func TestStripCursorMarker(t *testing.T) {
	got := stripCursorMarker("hi" + CursorMarker + "there")
	if got != "hithere" {
		t.Errorf("got %q", got)
	}
}
