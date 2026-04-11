package tui

import (
	"bytes"
	"strings"
	"sync"
)

// VirtualTerminal is an in-memory TerminalIO implementation for tests.
// Writes are accumulated into a buffer; the LastOutput / LatestFrame
// helpers make it easy to inspect what the TUI drew last.
type VirtualTerminal struct {
	mu       sync.Mutex
	width    int
	height   int
	buf      bytes.Buffer
	writes   [][]byte
	inputCh  chan []byte
	resizeCh chan struct{}
	stopped  bool
}

// NewVirtualTerminal returns a new in-memory terminal with the given
// dimensions and buffered channels.
func NewVirtualTerminal(width, height int) *VirtualTerminal {
	return &VirtualTerminal{
		width:    width,
		height:   height,
		inputCh:  make(chan []byte, 64),
		resizeCh: make(chan struct{}, 4),
	}
}

// Write records the bytes both in the aggregated buffer and the
// per-frame writes slice.
func (v *VirtualTerminal) Write(data []byte) (int, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	cp := make([]byte, len(data))
	copy(cp, data)
	v.writes = append(v.writes, cp)
	v.buf.Write(cp)
	return len(data), nil
}

// Width returns the current column count.
func (v *VirtualTerminal) Width() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.width
}

// Height returns the current row count.
func (v *VirtualTerminal) Height() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.height
}

// InputCh returns the read side of the input channel.
func (v *VirtualTerminal) InputCh() <-chan []byte { return v.inputCh }

// ResizeCh returns the read side of the resize channel.
func (v *VirtualTerminal) ResizeCh() <-chan struct{} { return v.resizeCh }

// Stop closes the input and resize channels. Safe to call more than
// once.
func (v *VirtualTerminal) Stop() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.stopped {
		return
	}
	v.stopped = true
	close(v.inputCh)
	close(v.resizeCh)
}

// SimulateInput pushes a byte sequence onto the input channel as if the
// user had typed it.
func (v *VirtualTerminal) SimulateInput(data []byte) {
	v.mu.Lock()
	if v.stopped {
		v.mu.Unlock()
		return
	}
	ch := v.inputCh
	v.mu.Unlock()
	ch <- data
}

// Resize updates the dimensions and fires a resize event.
func (v *VirtualTerminal) Resize(width, height int) {
	v.mu.Lock()
	v.width = width
	v.height = height
	if v.stopped {
		v.mu.Unlock()
		return
	}
	ch := v.resizeCh
	v.mu.Unlock()
	select {
	case ch <- struct{}{}:
	default:
	}
}

// Dump returns every byte written to the terminal since it was created.
func (v *VirtualTerminal) Dump() []byte {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make([]byte, v.buf.Len())
	copy(out, v.buf.Bytes())
	return out
}

// Writes returns a copy of every Write call that has been made.
func (v *VirtualTerminal) Writes() [][]byte {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make([][]byte, len(v.writes))
	for i, w := range v.writes {
		cp := make([]byte, len(w))
		copy(cp, w)
		out[i] = cp
	}
	return out
}

// Reset clears the write history. Useful between frames in a test.
func (v *VirtualTerminal) Reset() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.buf.Reset()
	v.writes = nil
}

// LastOutput returns the most recent Write call as a string. Empty when
// nothing has been written.
func (v *VirtualTerminal) LastOutput() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.writes) == 0 {
		return ""
	}
	return string(v.writes[len(v.writes)-1])
}

// FullOutput returns the concatenation of every Write since creation.
func (v *VirtualTerminal) FullOutput() string {
	return string(v.Dump())
}

// ContainsText reports whether the concatenated output contains the
// given substring, stripping ANSI escape sequences. Use for lightweight
// assertions like "the rendered frame includes the word 'ready'".
func (v *VirtualTerminal) ContainsText(needle string) bool {
	visible := stripANSI(v.FullOutput())
	return strings.Contains(visible, needle)
}

// stripANSI removes escape sequences so tests can assert on the visible
// text without worrying about styling noise.
func stripANSI(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] == 0x1B {
			if skip := escapeSkip(runes, i); skip > 0 {
				i += skip - 1
				continue
			}
		}
		b.WriteRune(runes[i])
	}
	return b.String()
}
