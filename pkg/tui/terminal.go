package tui

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"golang.org/x/term"
)

// Terminal is the real TerminalIO backend: it puts the tty into raw
// mode, enables bracketed paste, and starts a stdin reader + SIGWINCH
// watcher. All methods are safe to call concurrently.
type Terminal struct {
	in       *os.File
	out      *os.File
	oldState *term.State

	mu     sync.Mutex
	width  int
	height int

	inputCh  chan []byte
	resizeCh chan struct{}
	stopCh   chan struct{}
	stopOnce sync.Once
	started  bool
}

// NewTerminal constructs a Terminal using os.Stdin and os.Stdout. It
// does not yet enter raw mode — call Start for that.
func NewTerminal() *Terminal {
	return &Terminal{
		in:       os.Stdin,
		out:      os.Stdout,
		inputCh:  make(chan []byte, 64),
		resizeCh: make(chan struct{}, 8),
		stopCh:   make(chan struct{}),
	}
}

// Start puts the tty into raw mode, enables bracketed paste, and starts
// the input-reader and SIGWINCH goroutines. It is safe to call Start
// only once per Terminal instance.
func (t *Terminal) Start() error {
	if !term.IsTerminal(int(t.in.Fd())) {
		return fmt.Errorf("tui: stdin is not a terminal")
	}
	state, err := term.MakeRaw(int(t.in.Fd()))
	if err != nil {
		return fmt.Errorf("tui: make raw: %w", err)
	}
	t.oldState = state

	w, h, err := term.GetSize(int(t.out.Fd()))
	if err != nil {
		_ = term.Restore(int(t.in.Fd()), state)
		return fmt.Errorf("tui: get size: %w", err)
	}
	t.mu.Lock()
	t.width = w
	t.height = h
	t.started = true
	t.mu.Unlock()

	// Enable bracketed paste mode.
	_, _ = t.out.Write([]byte("\x1b[?2004h"))

	go t.readInputLoop()
	go t.watchResize()

	return nil
}

// Stop restores the tty to its original state and tells the background
// goroutines to exit. Safe to call multiple times.
func (t *Terminal) Stop() {
	t.stopOnce.Do(func() {
		close(t.stopCh)
		if t.oldState != nil {
			// Disable bracketed paste and restore cooked mode.
			_, _ = t.out.Write([]byte("\x1b[?2004l"))
			_ = term.Restore(int(t.in.Fd()), t.oldState)
		}
	})
}

// Write sends bytes to the tty.
func (t *Terminal) Write(data []byte) (int, error) {
	return t.out.Write(data)
}

// Width returns the current visible column count.
func (t *Terminal) Width() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.width
}

// Height returns the current visible row count.
func (t *Terminal) Height() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.height
}

// InputCh delivers parsed input sequences.
func (t *Terminal) InputCh() <-chan []byte { return t.inputCh }

// ResizeCh delivers terminal-resize signals.
func (t *Terminal) ResizeCh() <-chan struct{} { return t.resizeCh }

// readInputLoop pumps stdin into inputCh, splitting batched escape
// sequences into individual events.
func (t *Terminal) readInputLoop() {
	buf := make([]byte, 4096)
	for {
		select {
		case <-t.stopCh:
			return
		default:
		}
		n, err := t.in.Read(buf)
		if err != nil {
			return
		}
		if n == 0 {
			continue
		}
		for _, seq := range splitSequences(buf[:n]) {
			select {
			case t.inputCh <- seq:
			case <-t.stopCh:
				return
			}
		}
	}
}

// watchResize fires a struct{} on resizeCh whenever SIGWINCH arrives.
func (t *Terminal) watchResize() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGWINCH)
	defer signal.Stop(sigCh)
	for {
		select {
		case <-t.stopCh:
			return
		case <-sigCh:
			w, h, err := term.GetSize(int(t.out.Fd()))
			if err != nil {
				continue
			}
			t.mu.Lock()
			t.width = w
			t.height = h
			t.mu.Unlock()
			select {
			case t.resizeCh <- struct{}{}:
			default:
			}
		}
	}
}

// splitSequences splits a raw input batch into individual key events.
// It understands CSI (ESC [), SS3 (ESC O), APC/DCS/PM (ESC _/P/^), and
// single printable/ctrl characters.
func splitSequences(data []byte) [][]byte {
	var out [][]byte
	i := 0
	for i < len(data) {
		b := data[i]
		if b != 0x1B {
			// Single-byte token or a multi-byte UTF-8 rune.
			r, size := decodeRune(data[i:])
			if r == 0xFFFD && size == 1 {
				out = append(out, data[i:i+1])
				i++
				continue
			}
			out = append(out, data[i:i+size])
			i += size
			continue
		}

		// Escape byte.
		if i+1 >= len(data) {
			// Lone ESC — deliver as KeyEscape.
			out = append(out, data[i:i+1])
			i++
			continue
		}

		switch data[i+1] {
		case '[':
			// CSI: ends at any byte in 0x40..0x7E.
			end := findCSIEnd(data, i+2)
			out = append(out, data[i:end+1])
			i = end + 1
		case 'O':
			// SS3: ESC O + single char.
			if i+2 < len(data) {
				out = append(out, data[i:i+3])
				i += 3
			} else {
				out = append(out, data[i:i+2])
				i += 2
			}
		case '_', 'P', '^', ']':
			end := findSTEnd(data, i+2)
			out = append(out, data[i:end])
			i = end
		default:
			// ESC + char = alt+char.
			out = append(out, data[i:i+2])
			i += 2
		}
	}
	return out
}

// findCSIEnd returns the index of the terminator byte of a CSI
// sequence that starts at ESC [ (start points at the byte after '[').
func findCSIEnd(data []byte, start int) int {
	for j := start; j < len(data); j++ {
		b := data[j]
		if b >= '@' && b <= '~' {
			return j
		}
	}
	return len(data) - 1
}

// findSTEnd returns the index after a string-terminator (BEL or ESC \\)
// for an OSC / APC / DCS / PM sequence.
func findSTEnd(data []byte, start int) int {
	for j := start; j < len(data); j++ {
		if data[j] == 0x07 {
			return j + 1
		}
		if data[j] == 0x1B && j+1 < len(data) && data[j+1] == '\\' {
			return j + 2
		}
	}
	return len(data)
}

// decodeRune returns the next rune and its byte size, handling invalid
// UTF-8 by returning the replacement rune of size 1.
func decodeRune(data []byte) (rune, int) {
	if len(data) == 0 {
		return 0, 0
	}
	b := data[0]
	if b < 0x80 {
		return rune(b), 1
	}
	// Very small UTF-8 decoder — good enough for the 2-4 byte cases we
	// see in terminal input.
	var size int
	switch {
	case b&0xE0 == 0xC0:
		size = 2
	case b&0xF0 == 0xE0:
		size = 3
	case b&0xF8 == 0xF0:
		size = 4
	default:
		return 0xFFFD, 1
	}
	if len(data) < size {
		return 0xFFFD, 1
	}
	var r rune
	switch size {
	case 2:
		r = rune(b&0x1F)<<6 | rune(data[1]&0x3F)
	case 3:
		r = rune(b&0x0F)<<12 | rune(data[1]&0x3F)<<6 | rune(data[2]&0x3F)
	case 4:
		r = rune(b&0x07)<<18 | rune(data[1]&0x3F)<<12 | rune(data[2]&0x3F)<<6 | rune(data[3]&0x3F)
	}
	return r, size
}

// Compile-time assertion: Terminal satisfies TerminalIO.
var _ TerminalIO = (*Terminal)(nil)
