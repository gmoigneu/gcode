package components

import (
	"strings"
	"testing"

	"github.com/gmoigneu/gcode/pkg/tui"
)

func newFocusedEditor() *Editor {
	e := NewEditor(nil)
	e.SetFocused(true)
	return e
}

// ----- insertion -----

func TestEditorInsertText(t *testing.T) {
	e := newFocusedEditor()
	e.HandleInput([]byte("hello"))
	if e.Text() != "hello" {
		t.Errorf("text = %q", e.Text())
	}
	line, col := e.CursorPosition()
	if line != 0 || col != 5 {
		t.Errorf("cursor = %d,%d", line, col)
	}
}

func TestEditorInsertMidString(t *testing.T) {
	e := newFocusedEditor()
	e.SetText("helo")
	e.HandleInput([]byte("\x1b[D")) // left
	e.HandleInput([]byte("l"))
	if e.Text() != "hello" {
		t.Errorf("text = %q", e.Text())
	}
}

func TestEditorInsertNewline(t *testing.T) {
	e := newFocusedEditor()
	e.HandleInput([]byte("ab"))
	e.HandleInput([]byte("\x1b[13;2u")) // shift+enter (Kitty)
	e.HandleInput([]byte("cd"))
	if e.Text() != "ab\ncd" {
		t.Errorf("text = %q", e.Text())
	}
}

// ----- cursor movement -----

func TestEditorCursorMovement(t *testing.T) {
	e := newFocusedEditor()
	e.SetText("abc")
	e.HandleInput([]byte("\x1b[D"))
	e.HandleInput([]byte("\x1b[D"))
	_, col := e.CursorPosition()
	if col != 1 {
		t.Errorf("col = %d", col)
	}
	e.HandleInput([]byte("\x1b[C"))
	_, col = e.CursorPosition()
	if col != 2 {
		t.Errorf("col = %d", col)
	}
}

func TestEditorMoveLineStartEnd(t *testing.T) {
	e := newFocusedEditor()
	e.SetText("hello")
	e.HandleInput([]byte{0x01}) // ctrl+a
	_, col := e.CursorPosition()
	if col != 0 {
		t.Errorf("ctrl+a col = %d", col)
	}
	e.HandleInput([]byte{0x05}) // ctrl+e
	_, col = e.CursorPosition()
	if col != 5 {
		t.Errorf("ctrl+e col = %d", col)
	}
}

func TestEditorMoveWordLeftRight(t *testing.T) {
	e := newFocusedEditor()
	e.SetText("one two three")
	e.HandleInput([]byte{0x05})        // ctrl+e → col 13
	e.HandleInput([]byte("\x1b[1;3D")) // alt+left
	_, col := e.CursorPosition()
	if col != 8 { // start of "three"
		t.Errorf("col after word left = %d, want 8", col)
	}
	e.HandleInput([]byte("\x1b[1;3C")) // alt+right
	_, col = e.CursorPosition()
	if col != 13 {
		t.Errorf("col after word right = %d, want 13", col)
	}
}

func TestEditorMoveUpDown(t *testing.T) {
	e := newFocusedEditor()
	e.SetText("first\nsecond\nthird")
	e.HandleInput([]byte("\x1b[A")) // up — already on third line
	line, _ := e.CursorPosition()
	if line != 1 {
		t.Errorf("line = %d", line)
	}
	e.HandleInput([]byte("\x1b[A"))
	line, _ = e.CursorPosition()
	if line != 0 {
		t.Errorf("line = %d", line)
	}
	e.HandleInput([]byte("\x1b[B"))
	line, _ = e.CursorPosition()
	if line != 1 {
		t.Errorf("line = %d", line)
	}
}

// ----- deletion -----

func TestEditorBackspace(t *testing.T) {
	e := newFocusedEditor()
	e.HandleInput([]byte("abc"))
	e.HandleInput([]byte{0x7F})
	if e.Text() != "ab" {
		t.Errorf("text = %q", e.Text())
	}
}

func TestEditorBackspaceJoinLines(t *testing.T) {
	e := newFocusedEditor()
	e.SetText("ab\ncd")
	e.HandleInput([]byte("\x1b[B")) // down
	e.HandleInput([]byte{0x01})     // ctrl+a: line start
	e.HandleInput([]byte{0x7F})     // backspace
	if e.Text() != "abcd" {
		t.Errorf("text = %q", e.Text())
	}
}

func TestEditorDeleteForward(t *testing.T) {
	e := newFocusedEditor()
	e.SetText("abc")
	e.HandleInput([]byte{0x01}) // ctrl+a
	e.HandleInput([]byte{0x04}) // ctrl+d: delete fwd
	if e.Text() != "bc" {
		t.Errorf("text = %q", e.Text())
	}
}

func TestEditorDeleteWordBack(t *testing.T) {
	e := newFocusedEditor()
	e.SetText("hello world")
	e.HandleInput([]byte{0x05}) // ctrl+e
	e.HandleInput([]byte{0x17}) // ctrl+w
	if e.Text() != "hello " {
		t.Errorf("text = %q", e.Text())
	}
}

func TestEditorDeleteToLineStart(t *testing.T) {
	e := newFocusedEditor()
	e.SetText("hello")
	e.HandleInput([]byte{0x05}) // ctrl+e
	e.HandleInput([]byte{0x15}) // ctrl+u
	if e.Text() != "" {
		t.Errorf("text = %q", e.Text())
	}
}

func TestEditorDeleteToLineEnd(t *testing.T) {
	e := newFocusedEditor()
	e.SetText("hello world")
	e.HandleInput([]byte{0x01}) // ctrl+a
	e.HandleInput([]byte{0x0B}) // ctrl+k
	if e.Text() != "" {
		t.Errorf("text = %q", e.Text())
	}
}

// ----- kill ring / yank -----

func TestEditorKillYank(t *testing.T) {
	e := newFocusedEditor()
	e.SetText("hello world")
	e.HandleInput([]byte{0x05}) // ctrl+e
	e.HandleInput([]byte{0x17}) // ctrl+w (kill word backward: "world")
	// Cursor at col 6.
	e.HandleInput([]byte{0x19}) // ctrl+y
	if e.Text() != "hello world" {
		t.Errorf("text = %q", e.Text())
	}
}

// ----- undo -----

func TestEditorUndo(t *testing.T) {
	e := newFocusedEditor()
	e.HandleInput([]byte("a"))
	e.HandleInput([]byte("b"))
	if e.Text() != "ab" {
		t.Errorf("text = %q", e.Text())
	}
	e.HandleInput([]byte{0x1F}) // ctrl+_ undo
	if e.Text() != "a" {
		t.Errorf("after undo = %q", e.Text())
	}
}

// ----- submit / change -----

func TestEditorSubmitCallback(t *testing.T) {
	e := newFocusedEditor()
	got := ""
	e.OnSubmit = func(text string) { got = text }
	e.HandleInput([]byte("hello"))
	e.HandleInput([]byte{0x0D}) // enter
	if got != "hello" {
		t.Errorf("submit got %q", got)
	}
}

func TestEditorChangeCallback(t *testing.T) {
	e := newFocusedEditor()
	var changes []string
	e.OnChange = func(text string) { changes = append(changes, text) }
	e.HandleInput([]byte("a"))
	e.HandleInput([]byte("b"))
	if len(changes) < 2 {
		t.Errorf("changes = %v", changes)
	}
}

// ----- render -----

func TestEditorRenderCursorMarker(t *testing.T) {
	e := newFocusedEditor()
	e.SetText("abc")
	e.HandleInput([]byte("\x1b[D")) // move left one
	rendered := e.Render(80)
	if !strings.Contains(rendered[0], tui.CursorMarker) {
		t.Errorf("cursor marker missing: %q", rendered[0])
	}
}

func TestEditorRenderWordWrap(t *testing.T) {
	e := newFocusedEditor()
	e.SetText("hello world gcode")
	lines := e.Render(7)
	if len(lines) < 2 {
		t.Errorf("expected wrap, got %v", lines)
	}
}

func TestEditorRenderEmpty(t *testing.T) {
	e := newFocusedEditor()
	lines := e.Render(10)
	if len(lines) != 1 || (lines[0] != "" && !strings.Contains(lines[0], tui.CursorMarker)) {
		t.Errorf("got %v", lines)
	}
}

func TestEditorFocusRequiredForInput(t *testing.T) {
	e := NewEditor(nil) // not focused
	e.HandleInput([]byte("hello"))
	if e.Text() != "" {
		t.Error("unfocused editor should ignore input")
	}
}
