package tui

// Component is the core interface every TUI element implements. Render
// returns the component's visual state as a slice of lines (one string per
// terminal row). Each line may contain ANSI escape codes; the TUI's
// differential renderer compares these strings to decide what to redraw.
//
// width is the available number of columns. Components should never
// return lines whose visible width exceeds width — the TUI's width guard
// will fail the frame if they do.
//
// Invalidate forces the component to discard any cached render state.
type Component interface {
	Render(width int) []string
	Invalidate()
}

// InputHandler is implemented by components that accept raw keyboard
// input. The bytes are the terminal escape sequence (or UTF-8 bytes) for
// the key that was pressed.
type InputHandler interface {
	HandleInput(data []byte)
}

// Focusable is implemented by components that can be focused. Only one
// component is "focused" in a given TUI instance at a time; the TUI
// routes input to the focused component.
type Focusable interface {
	SetFocused(focused bool)
	IsFocused() bool
}

// CursorMarker is an APC escape sequence used by focused components to
// indicate where the terminal's hardware cursor should be positioned.
// The TUI scans for this marker during composition, strips it, and
// issues a real cursor-positioning escape there.
const CursorMarker = "\x1b_gc:c\x07"

// Reset is the ANSI escape for clearing all SGR attributes. Exposed so
// components can append it to the end of each line without importing the
// constant from a deeper file.
const Reset = "\x1b[0m"
