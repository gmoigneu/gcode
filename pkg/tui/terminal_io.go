package tui

// TerminalIO is the narrow interface the TUI uses to talk to a terminal.
// The real terminal (wrapping /dev/tty) and the virtual terminal (for
// tests) both satisfy it, so the TUI never has to know which backend is
// in use.
type TerminalIO interface {
	// Write sends bytes to the terminal output (escape sequences + text).
	Write(data []byte) (int, error)

	// Width returns the current visible width in columns.
	Width() int

	// Height returns the current visible height in rows.
	Height() int

	// InputCh delivers raw input — one batch per key press or paste.
	InputCh() <-chan []byte

	// ResizeCh delivers a signal whenever the terminal size changes.
	ResizeCh() <-chan struct{}

	// Stop performs any cleanup the backend needs (restore cooked mode,
	// stop goroutines, etc).
	Stop()
}
