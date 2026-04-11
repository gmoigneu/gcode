// Package tui is a lightweight terminal UI framework with differential
// rendering, component composition, overlays, and keyboard-driven input.
package tui

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// InputListener can intercept or rewrite raw input before it reaches the
// focused component. Returning consumed=true drops the input; returning
// rewritten != nil replaces it.
type InputListener func(data []byte) (consumed bool, rewritten []byte)

// OverlayOptions configures how an overlay is positioned and sized.
type OverlayOptions struct {
	Width     int
	MinWidth  int
	MaxHeight int
	// Col / Row are absolute positions. When negative (default -1),
	// Anchor + OffsetX/OffsetY are used instead.
	Col     int
	Row     int
	Anchor  string
	OffsetX int
	OffsetY int
}

// OverlayHandle is returned by ShowOverlay so callers can hide or
// remove the overlay later.
type OverlayHandle struct {
	tui   *TUI
	entry *overlayEntry
}

type overlayEntry struct {
	component Component
	options   OverlayOptions
	hidden    bool
	order     int
}

// TUI wires a Component tree to a TerminalIO backend. It owns the main
// render loop, differential rendering, and overlay compositing.
type TUI struct {
	terminal TerminalIO
	root     Component

	focused Component

	mu             sync.Mutex
	previousLines  []string
	previousWidth  int
	previousHeight int
	showCursor     bool

	overlays []*overlayEntry
	overlayN int

	listeners []InputListener

	stopCh          chan struct{}
	stopOnce        sync.Once
	renderRequested bool
	lastRender      time.Time

	minRenderInterval time.Duration
}

// New wires a TUI to a terminal backend. The root component is the
// top-level tree the TUI renders.
func New(terminal TerminalIO, root Component) *TUI {
	return &TUI{
		terminal:          terminal,
		root:              root,
		showCursor:        true,
		stopCh:            make(chan struct{}),
		minRenderInterval: 16 * time.Millisecond,
	}
}

// SetRoot replaces the root component and triggers a re-render.
func (t *TUI) SetRoot(root Component) {
	t.mu.Lock()
	t.root = root
	t.mu.Unlock()
	t.Render()
}

// Terminal exposes the underlying backend.
func (t *TUI) Terminal() TerminalIO { return t.terminal }

// SetFocus marks a component as the input target.
func (t *TUI) SetFocus(c Component) {
	t.mu.Lock()
	if prev, ok := t.focused.(Focusable); ok {
		prev.SetFocused(false)
	}
	t.focused = c
	if next, ok := c.(Focusable); ok {
		next.SetFocused(true)
	}
	t.mu.Unlock()
}

// Focused returns the currently focused component (or nil).
func (t *TUI) Focused() Component {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.focused
}

// AddInputListener registers a raw-input interceptor. Listeners are
// invoked in registration order.
func (t *TUI) AddInputListener(l InputListener) {
	t.mu.Lock()
	t.listeners = append(t.listeners, l)
	t.mu.Unlock()
}

// ShowOverlay adds an overlay component. Overlays render on top of the
// main tree; the most recently added overlay is on top.
func (t *TUI) ShowOverlay(c Component, opts OverlayOptions) *OverlayHandle {
	t.mu.Lock()
	t.overlayN++
	entry := &overlayEntry{component: c, options: opts, order: t.overlayN}
	t.overlays = append(t.overlays, entry)
	t.mu.Unlock()
	t.Render()
	return &OverlayHandle{tui: t, entry: entry}
}

// Hide marks the overlay hidden but keeps it in the list.
func (h *OverlayHandle) Hide() {
	if h == nil || h.entry == nil {
		return
	}
	h.tui.mu.Lock()
	h.entry.hidden = true
	h.tui.mu.Unlock()
	h.tui.Render()
}

// Show unhides a previously hidden overlay.
func (h *OverlayHandle) Show() {
	if h == nil || h.entry == nil {
		return
	}
	h.tui.mu.Lock()
	h.entry.hidden = false
	h.tui.mu.Unlock()
	h.tui.Render()
}

// Close removes the overlay entirely.
func (h *OverlayHandle) Close() {
	if h == nil || h.entry == nil {
		return
	}
	h.tui.mu.Lock()
	for i, e := range h.tui.overlays {
		if e == h.entry {
			h.tui.overlays = append(h.tui.overlays[:i], h.tui.overlays[i+1:]...)
			break
		}
	}
	h.tui.mu.Unlock()
	h.tui.Render()
}

// Render performs a single render pass. Rate-limited to one frame every
// minRenderInterval.
func (t *TUI) Render() {
	t.mu.Lock()
	now := time.Now()
	if now.Sub(t.lastRender) < t.minRenderInterval {
		t.renderRequested = true
		t.mu.Unlock()
		return
	}
	t.lastRender = now
	t.renderRequested = false
	t.mu.Unlock()
	t.doRender()
}

// ForceRender clears the cached state and re-renders from scratch.
func (t *TUI) ForceRender() {
	t.mu.Lock()
	t.previousLines = nil
	t.previousWidth = 0
	t.previousHeight = 0
	t.lastRender = time.Now()
	t.mu.Unlock()
	t.doRender()
}

// Start begins the input + resize loop. Call Stop to exit.
func (t *TUI) Start() {
	t.ForceRender()
	go t.mainLoop()
}

// Stop ends the main loop. Safe to call multiple times.
func (t *TUI) Stop() {
	t.stopOnce.Do(func() {
		close(t.stopCh)
		if t.terminal != nil {
			t.terminal.Stop()
		}
	})
}

func (t *TUI) mainLoop() {
	input := t.terminal.InputCh()
	resize := t.terminal.ResizeCh()
	for {
		select {
		case <-t.stopCh:
			return
		case data, ok := <-input:
			if !ok {
				return
			}
			t.handleInput(data)
		case _, ok := <-resize:
			if !ok {
				return
			}
			t.ForceRender()
		}
	}
}

func (t *TUI) handleInput(data []byte) {
	t.mu.Lock()
	listeners := append([]InputListener(nil), t.listeners...)
	focused := t.focused
	t.mu.Unlock()

	for _, l := range listeners {
		consumed, rewritten := l(data)
		if consumed {
			t.Render()
			return
		}
		if rewritten != nil {
			data = rewritten
		}
	}

	if IsKeyRelease(data) {
		return
	}
	if handler, ok := focused.(InputHandler); ok {
		handler.HandleInput(data)
	}
	t.Render()
}

// doRender is the core render routine. It is intentionally linear to
// keep the differential rendering logic easy to follow.
func (t *TUI) doRender() {
	t.mu.Lock()
	root := t.root
	width := t.terminal.Width()
	height := t.terminal.Height()

	if root == nil || width <= 0 {
		t.mu.Unlock()
		return
	}

	widthChanged := width != t.previousWidth
	heightChanged := height != t.previousHeight

	baseLines := root.Render(width)
	overlays := append([]*overlayEntry(nil), t.overlays...)
	t.mu.Unlock()

	// Composite overlays.
	composited := t.compositeOverlays(baseLines, overlays, width, height)

	// Extract cursor marker and append SGR reset.
	cursorRow, cursorCol := extractCursor(composited)
	for i := range composited {
		composited[i] = stripCursorMarker(composited[i]) + Reset
	}

	// Width overflow guard (clip silently — components are responsible
	// for respecting the width, but clipping keeps broken components
	// from tearing the terminal).
	for i, line := range composited {
		if VisibleWidth(line) > width {
			composited[i] = SliceByColumn(line, 0, width) + Reset
		}
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	needsFull := t.previousLines == nil || widthChanged || heightChanged
	var out strings.Builder

	if needsFull {
		out.WriteString("\x1b[2J\x1b[H")
		for i, line := range composited {
			if i > 0 {
				out.WriteString("\r\n")
			}
			out.WriteString(line)
		}
	} else {
		var diffs []int
		maxLen := len(composited)
		if len(t.previousLines) > maxLen {
			maxLen = len(t.previousLines)
		}
		for i := 0; i < maxLen; i++ {
			var a, b string
			if i < len(composited) {
				a = composited[i]
			}
			if i < len(t.previousLines) {
				b = t.previousLines[i]
			}
			if a != b {
				diffs = append(diffs, i)
			}
		}
		if len(diffs) > 0 {
			out.WriteString("\x1b[H") // home cursor for simplicity
			for i := 0; i < len(composited); i++ {
				if i > 0 {
					out.WriteString("\r\n")
				}
				out.WriteString("\x1b[2K" + composited[i])
			}
			// Clear any trailing lines left over from a longer previous frame.
			for i := len(composited); i < len(t.previousLines); i++ {
				out.WriteString("\r\n\x1b[2K")
			}
		}
	}

	// Cursor positioning.
	if cursorRow >= 0 && t.showCursor {
		fmt.Fprintf(&out, "\x1b[%d;%dH", cursorRow+1, cursorCol+1)
		out.WriteString("\x1b[?25h")
	} else {
		out.WriteString("\x1b[?25l")
	}

	if out.Len() > 0 {
		_, _ = t.terminal.Write([]byte(out.String()))
	}

	t.previousLines = composited
	t.previousWidth = width
	t.previousHeight = height
}

// compositeOverlays splices overlay components into the base frame.
func (t *TUI) compositeOverlays(base []string, overlays []*overlayEntry, width, height int) []string {
	if len(overlays) == 0 {
		return base
	}
	out := make([]string, len(base))
	copy(out, base)

	for _, e := range overlays {
		if e.hidden || e.component == nil {
			continue
		}
		w := e.options.Width
		if w <= 0 {
			w = width / 2
			if w < e.options.MinWidth {
				w = e.options.MinWidth
			}
		}
		if w > width {
			w = width
		}
		lines := e.component.Render(w)
		if e.options.MaxHeight > 0 && len(lines) > e.options.MaxHeight {
			lines = lines[:e.options.MaxHeight]
		}
		row, col := resolveOverlayPosition(e.options, width, height, w, len(lines))

		for len(out) < row+len(lines) {
			out = append(out, "")
		}
		for i, line := range lines {
			r := row + i
			out[r] = compositeLineAt(out[r], line, col, width)
		}
	}
	return out
}

func resolveOverlayPosition(opts OverlayOptions, width, height, overlayW, overlayH int) (row, col int) {
	// Explicit positions (non-negative) take precedence.
	if opts.Row >= 0 {
		row = opts.Row
	}
	if opts.Col >= 0 {
		col = opts.Col
	}
	anchor := opts.Anchor
	if anchor == "" && opts.Row < 0 && opts.Col < 0 {
		anchor = "center"
	}
	switch anchor {
	case "center":
		row = (height - overlayH) / 2
		if row < 0 {
			row = 0
		}
		col = (width - overlayW) / 2
		if col < 0 {
			col = 0
		}
	case "top-left":
		row = 0
		col = 0
	case "top-right":
		row = 0
		col = width - overlayW
	case "bottom-left":
		row = height - overlayH
		col = 0
	case "bottom-right":
		row = height - overlayH
		col = width - overlayW
	}
	row += opts.OffsetY
	col += opts.OffsetX
	if row < 0 {
		row = 0
	}
	if col < 0 {
		col = 0
	}
	return row, col
}

func compositeLineAt(baseLine, overlayLine string, col, totalWidth int) string {
	overlayW := VisibleWidth(overlayLine)
	if col >= totalWidth || overlayW == 0 {
		return baseLine
	}
	before := SliceByColumn(baseLine, 0, col)
	before = PadToWidth(before, col)
	after := SliceByColumn(baseLine, col+overlayW, totalWidth)
	return before + Reset + overlayLine + Reset + after
}

// extractCursor scans composited lines for CursorMarker, returning the
// (row, col) position of the cursor or (-1, -1) if none is present.
func extractCursor(lines []string) (int, int) {
	for i, line := range lines {
		idx := strings.Index(line, CursorMarker)
		if idx < 0 {
			continue
		}
		prefix := line[:idx]
		return i, VisibleWidth(prefix)
	}
	return -1, -1
}

// stripCursorMarker removes any CursorMarker occurrences from a line.
func stripCursorMarker(line string) string {
	for {
		idx := strings.Index(line, CursorMarker)
		if idx < 0 {
			return line
		}
		line = line[:idx] + line[idx+len(CursorMarker):]
	}
}
