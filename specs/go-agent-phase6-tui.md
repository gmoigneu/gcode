# Phase 6: Terminal UI framework (`pkg/tui`)

> Part of gcode. Go 1.24+. Custom TUI from scratch, no external TUI frameworks (no Bubble Tea, no tcell).

Custom TUI with differential rendering, inspired by pi-tui. No external TUI library. Only depends on `golang.org/x/term` for raw mode and `github.com/mattn/go-runewidth` for character width.

## 6.1 Component model (`pkg/tui/types.go`)

```go
// Component is the core interface. Every UI element implements this.
type Component interface {
    // Render returns an array of strings (one per line).
    // Each string may contain ANSI escape codes.
    // Width is the available terminal columns.
    Render(width int) []string

    // Invalidate clears cached render state, forcing re-render.
    Invalidate()
}

// InputHandler is implemented by components that accept keyboard input.
type InputHandler interface {
    HandleInput(data []byte)
}

// Focusable is implemented by components that can receive focus.
type Focusable interface {
    SetFocused(focused bool)
    IsFocused() bool
}
```

Components return `[]string` from `Render`. Each string is one terminal line. The TUI compares these strings between frames for differential rendering.

### Cursor marker

```go
// CursorMarker is an APC escape sequence used by focused components
// to indicate where the hardware cursor should be positioned.
// The TUI finds this, strips it, and positions the real cursor there.
const CursorMarker = "\x1b_pi:c\x07"
```

## 6.2 Terminal abstraction (`pkg/tui/terminal.go`)

```go
type Terminal struct {
    in       *os.File
    out      *os.File
    oldState *term.State
    width    int
    height   int
    kitty    bool // Kitty keyboard protocol active

    inputCh  chan []byte
    resizeCh chan struct{}
    stopCh   chan struct{}
}

func NewTerminal() *Terminal

// Start enters raw mode, enables bracketed paste, attempts Kitty keyboard protocol.
func (t *Terminal) Start() error

// Stop restores the terminal to its original state.
func (t *Terminal) Stop()

// Write outputs data to the terminal.
func (t *Terminal) Write(data []byte) (int, error)

// Width returns current terminal width.
func (t *Terminal) Width() int

// Height returns current terminal height.
func (t *Terminal) Height() int

// InputCh returns a channel that receives raw input bytes.
func (t *Terminal) InputCh() <-chan []byte

// ResizeCh returns a channel that signals terminal resize.
func (t *Terminal) ResizeCh() <-chan struct{}
```

### Start algorithm

1. Get current terminal state: `term.GetState(fd)`.
2. Enter raw mode: `term.MakeRaw(fd)`.
3. Enable bracketed paste: write `\x1b[?2004h`.
4. Attempt Kitty keyboard protocol: write `\x1b[>7u` (flags: disambiguate + event types + alternate keys).
   - Wait 150ms for response. If terminal echoes back a Kitty CSI sequence, mark `kitty = true`.
   - If no response, fall back to xterm modifyOtherKeys: write `\x1b[>4;2m`.
5. Query terminal size: `term.GetSize(fd)`.
6. Start goroutine for stdin reading (see below).
7. Start goroutine for SIGWINCH handling.

### Input reading goroutine

```go
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
        if n > 0 {
            // Split batched input into individual escape sequences
            sequences := splitSequences(buf[:n])
            for _, seq := range sequences {
                t.inputCh <- seq
            }
        }
    }
}
```

### Sequence splitting

Terminal input can arrive batched (multiple escape sequences in one read). Split them:

```go
// splitSequences splits batched terminal input into individual sequences.
// Rules:
// - ESC followed by [ or O starts a CSI/SS3 sequence (ends at letter)
// - ESC followed by _ starts an APC sequence (ends at ST)
// - ESC alone is the escape key (after a short delay)
// - Other bytes are individual characters or UTF-8 sequences
func splitSequences(data []byte) [][]byte
```

### Resize handling

```go
func (t *Terminal) watchResize() {
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGWINCH)
    for {
        select {
        case <-sigCh:
            w, h, _ := term.GetSize(int(t.out.Fd()))
            t.width = w
            t.height = h
            t.resizeCh <- struct{}{}
        case <-t.stopCh:
            signal.Stop(sigCh)
            return
        }
    }
}
```

## 6.3 Key parsing (`pkg/tui/keys.go`)

Two parallel parsing paths, matching pi's `keys.ts`.

```go
type KeyID string

// Key constants
const (
    KeyEscape    KeyID = "escape"
    KeyEnter     KeyID = "enter"
    KeyTab       KeyID = "tab"
    KeyBackspace KeyID = "backspace"
    KeyDelete    KeyID = "delete"
    KeyUp        KeyID = "up"
    KeyDown      KeyID = "down"
    KeyLeft      KeyID = "left"
    KeyRight     KeyID = "right"
    KeyHome      KeyID = "home"
    KeyEnd       KeyID = "end"
    KeyPageUp    KeyID = "pageup"
    KeyPageDown  KeyID = "pagedown"
    KeySpace     KeyID = "space"
    // ... letters a-z, digits 0-9, symbols
)

// Modifier combinations as KeyID strings:
// "ctrl+c", "shift+enter", "alt+d", "ctrl+shift+p", etc.

// MatchesKey checks if raw terminal input matches a key identifier.
func MatchesKey(data []byte, key KeyID) bool

// ParseKey attempts to parse raw input into a KeyID.
func ParseKey(data []byte) (KeyID, bool)

// IsKeyRelease checks if the input is a Kitty key release event.
func IsKeyRelease(data []byte) bool
```

### Kitty protocol parsing

CSI-u format: `ESC [ codepoint ; modifiers u`

```go
func parseKittySequence(data []byte) (codepoint int, mods int, eventType int, ok bool)
```

Modifiers: shift=1, alt=2, ctrl=4 (bitmask, 1-indexed: actual_mod = reported - 1).

Event types: 1=press, 2=repeat, 3=release.

### Legacy sequence tables

Maps of byte sequences to key IDs for VT100/xterm terminals:

```go
var legacySequences = map[string]KeyID{
    "\x1b[A":    KeyUp,
    "\x1b[B":    KeyDown,
    "\x1b[C":    KeyRight,
    "\x1b[D":    KeyLeft,
    "\x1b[H":    KeyHome,
    "\x1b[F":    KeyEnd,
    "\x1b[1;2A": "shift+up",
    "\x1b[1;5A": "ctrl+up",
    // ... exhaustive table
}
```

### Ctrl+char mapping

```go
// ctrlChar returns the raw byte for ctrl+letter.
// ctrl+a = 0x01, ctrl+b = 0x02, ..., ctrl+z = 0x1a
func ctrlChar(letter byte) byte {
    return letter & 0x1f
}
```

## 6.4 Keybindings (`pkg/tui/keybindings.go`)

```go
type KeybindingID string

const (
    // Editor keybindings
    KBCursorUp          KeybindingID = "tui.editor.cursorUp"
    KBCursorDown        KeybindingID = "tui.editor.cursorDown"
    KBCursorLeft        KeybindingID = "tui.editor.cursorLeft"
    KBCursorRight       KeybindingID = "tui.editor.cursorRight"
    KBCursorWordLeft    KeybindingID = "tui.editor.cursorWordLeft"
    KBCursorWordRight   KeybindingID = "tui.editor.cursorWordRight"
    KBCursorLineStart   KeybindingID = "tui.editor.cursorLineStart"
    KBCursorLineEnd     KeybindingID = "tui.editor.cursorLineEnd"
    KBDeleteCharBack    KeybindingID = "tui.editor.deleteCharBackward"
    KBDeleteCharFwd     KeybindingID = "tui.editor.deleteCharForward"
    KBDeleteWordBack    KeybindingID = "tui.editor.deleteWordBackward"
    KBDeleteWordFwd     KeybindingID = "tui.editor.deleteWordForward"
    KBDeleteToLineStart KeybindingID = "tui.editor.deleteToLineStart"
    KBDeleteToLineEnd   KeybindingID = "tui.editor.deleteToLineEnd"
    KBYank              KeybindingID = "tui.editor.yank"
    KBYankPop           KeybindingID = "tui.editor.yankPop"
    KBUndo              KeybindingID = "tui.editor.undo"
    KBNewLine           KeybindingID = "tui.input.newLine"
    KBSubmit            KeybindingID = "tui.input.submit"
    KBSelectUp          KeybindingID = "tui.select.up"
    KBSelectDown        KeybindingID = "tui.select.down"
    KBSelectConfirm     KeybindingID = "tui.select.confirm"
    KBSelectCancel      KeybindingID = "tui.select.cancel"
)

type KeybindingDef struct {
    DefaultKeys []KeyID
    Description string
}

var DefaultKeybindings = map[KeybindingID]KeybindingDef{
    KBCursorUp:          {DefaultKeys: []KeyID{KeyUp, "ctrl+p"}, Description: "Move cursor up"},
    KBCursorDown:        {DefaultKeys: []KeyID{KeyDown, "ctrl+n"}, Description: "Move cursor down"},
    KBCursorLeft:        {DefaultKeys: []KeyID{KeyLeft, "ctrl+b"}, Description: "Move cursor left"},
    KBCursorRight:       {DefaultKeys: []KeyID{KeyRight, "ctrl+f"}, Description: "Move cursor right"},
    KBCursorWordLeft:    {DefaultKeys: []KeyID{"alt+left", "alt+b"}, Description: "Move cursor word left"},
    KBCursorWordRight:   {DefaultKeys: []KeyID{"alt+right", "alt+f"}, Description: "Move cursor word right"},
    KBCursorLineStart:   {DefaultKeys: []KeyID{KeyHome, "ctrl+a"}, Description: "Move to line start"},
    KBCursorLineEnd:     {DefaultKeys: []KeyID{KeyEnd, "ctrl+e"}, Description: "Move to line end"},
    KBDeleteCharBack:    {DefaultKeys: []KeyID{KeyBackspace}, Description: "Delete char backward"},
    KBDeleteCharFwd:     {DefaultKeys: []KeyID{KeyDelete, "ctrl+d"}, Description: "Delete char forward"},
    KBDeleteWordBack:    {DefaultKeys: []KeyID{"ctrl+w", "alt+backspace"}, Description: "Delete word backward"},
    KBDeleteWordFwd:     {DefaultKeys: []KeyID{"alt+d"}, Description: "Delete word forward"},
    KBDeleteToLineStart: {DefaultKeys: []KeyID{"ctrl+u"}, Description: "Delete to line start"},
    KBDeleteToLineEnd:   {DefaultKeys: []KeyID{"ctrl+k"}, Description: "Delete to line end"},
    KBYank:              {DefaultKeys: []KeyID{"ctrl+y"}, Description: "Yank (paste from kill ring)"},
    KBYankPop:           {DefaultKeys: []KeyID{"alt+y"}, Description: "Cycle kill ring"},
    KBUndo:              {DefaultKeys: []KeyID{"ctrl+-"}, Description: "Undo"},
    KBNewLine:           {DefaultKeys: []KeyID{"shift+enter"}, Description: "Insert newline"},
    KBSubmit:            {DefaultKeys: []KeyID{KeyEnter}, Description: "Submit"},
    KBSelectUp:          {DefaultKeys: []KeyID{KeyUp, "ctrl+p"}, Description: "Select previous"},
    KBSelectDown:        {DefaultKeys: []KeyID{KeyDown, "ctrl+n"}, Description: "Select next"},
    KBSelectConfirm:     {DefaultKeys: []KeyID{KeyEnter}, Description: "Confirm selection"},
    KBSelectCancel:      {DefaultKeys: []KeyID{KeyEscape, "ctrl+c"}, Description: "Cancel selection"},
}

type KeybindingsManager struct {
    definitions  map[KeybindingID]KeybindingDef
    userOverrides map[KeybindingID][]KeyID
    resolved     map[KeybindingID][]KeyID
}

func NewKeybindingsManager() *KeybindingsManager

// Matches checks if raw input matches any key bound to a keybinding ID.
func (m *KeybindingsManager) Matches(data []byte, id KeybindingID) bool
```

## 6.5 ANSI-aware string width (`pkg/tui/width.go`)

```go
// VisibleWidth calculates the visible width of a string, ignoring ANSI escape codes.
func VisibleWidth(s string) int

// SliceByColumn slices a string to fit within maxWidth visible columns,
// preserving ANSI escape codes.
func SliceByColumn(s string, startCol, endCol int) string

// PadToWidth pads a string to the target visible width with spaces.
func PadToWidth(s string, targetWidth int) string
```

`VisibleWidth` must handle:
- CSI sequences (`\x1b[...m` for SGR, `\x1b[...A/B/C/D` for cursor movement)
- OSC sequences (`\x1b]...ST` for hyperlinks)
- APC sequences (`\x1b_...ST`)
- East Asian wide characters (use `go-runewidth`)

Implementation: state machine that tracks "inside escape sequence" and skips those bytes from the width count.

## 6.6 Container (`pkg/tui/components/container.go`)

```go
type Container struct {
    children []Component
    mu       sync.RWMutex
}

func NewContainer() *Container

func (c *Container) AddChild(child Component)
func (c *Container) RemoveChild(child Component)
func (c *Container) Clear()

func (c *Container) Render(width int) []string {
    c.mu.RLock()
    defer c.mu.RUnlock()
    var lines []string
    for _, child := range c.children {
        lines = append(lines, child.Render(width)...)
    }
    return lines
}

func (c *Container) Invalidate() {
    c.mu.RLock()
    defer c.mu.RUnlock()
    for _, child := range c.children {
        child.Invalidate()
    }
}
```

## 6.7 TUI struct and differential renderer (`pkg/tui/tui.go`)

```go
type TUI struct {
    Container // embedded

    terminal        *Terminal
    focused         Component // receives input
    previousLines   []string
    previousWidth   int
    previousHeight  int
    cursorRow       int
    hardwareCursorRow int
    maxLinesRendered int
    showCursor       bool
    clearOnShrink    bool

    overlays        []*overlayEntry
    inputListeners  []InputListener

    renderRequested bool
    renderTimer     *time.Timer
    renderMu        sync.Mutex
    stopped         bool

    minRenderInterval time.Duration // 16ms = 60fps
}

type InputListener func(data []byte) (consumed bool, rewritten []byte)

type overlayEntry struct {
    component Component
    options   OverlayOptions
    preFocus  Component
    hidden    bool
    order     int
}

type OverlayOptions struct {
    Width     int    // absolute columns, 0 = auto
    MinWidth  int
    MaxHeight int
    Anchor    string // "center", "top-left", "top-right", "bottom-left", etc.
    Row       int    // absolute row, -1 = use anchor
    Col       int    // absolute col, -1 = use anchor
    OffsetX   int
    OffsetY   int
    Margin    int
}

func New(terminal *Terminal) *TUI

// Start begins the render/input loop.
func (t *TUI) Start()

// Stop exits the render/input loop and restores the terminal.
func (t *TUI) Stop()

// SetFocus sets the focused component.
func (t *TUI) SetFocus(c Component)

// Liveness event subscription: the TUI subscribes to agent LivenessUpdate
// events (thinking, executing, stalled, idle) via an event channel. When a
// LivenessUpdate arrives, the TUI updates the StatusBar and Loader state
// and calls RequestRender() to push the new status to the screen. This
// keeps the status bar and loader in sync with agent activity without
// polling.

// ShowOverlay adds an overlay component.
func (t *TUI) ShowOverlay(c Component, opts OverlayOptions) *OverlayHandle

// RequestRender schedules a render (rate-limited to 60fps).
func (t *TUI) RequestRender()

// ForceRender clears all cached state and renders from scratch.
func (t *TUI) ForceRender()

// AddInputListener registers a raw input interceptor.
func (t *TUI) AddInputListener(listener InputListener)
```

### Main loop

```go
func (t *TUI) Start() {
    go func() {
        for {
            select {
            case data := <-t.terminal.InputCh():
                t.handleInput(data)
            case <-t.terminal.ResizeCh():
                t.ForceRender()
            case <-t.stopCh:
                return
            }
        }
    }()
}
```

### handleInput

```go
func (t *TUI) handleInput(data []byte) {
    // 1. Run input listeners (can consume or rewrite)
    for _, listener := range t.inputListeners {
        consumed, rewritten := listener(data)
        if consumed {
            t.RequestRender()
            return
        }
        if rewritten != nil {
            data = rewritten
        }
    }

    // 2. Filter key release events (unless component opts in)
    if IsKeyRelease(data) {
        return
    }

    // 3. Forward to focused component
    if handler, ok := t.focused.(InputHandler); ok {
        handler.HandleInput(data)
    }

    t.RequestRender()
}
```

### doRender (differential rendering)

```go
func (t *TUI) doRender() {
    t.renderMu.Lock()
    defer t.renderMu.Unlock()

    width := t.terminal.Width()
    height := t.terminal.Height()
    widthChanged := width != t.previousWidth
    heightChanged := height != t.previousHeight

    // 1. Render component tree
    newLines := t.Render(width)

    // 2. Composite overlays
    newLines = t.compositeOverlays(newLines, width, height)

    // 3. Extract and strip cursor marker
    cursorRow, cursorCol := extractCursorPosition(newLines)

    // 4. Append ANSI reset to every line
    for i := range newLines {
        newLines[i] += "\x1b[0m"
    }

    // 5. Determine render strategy
    needsFullRedraw := t.previousLines == nil || widthChanged || heightChanged

    var output strings.Builder

    // Synchronized output start
    output.WriteString("\x1b[?2026h")

    if needsFullRedraw {
        // Clear screen and write all lines
        output.WriteString("\x1b[2J\x1b[H") // clear + home
        for i, line := range newLines {
            if i > 0 {
                output.WriteString("\r\n")
            }
            output.WriteString(line)
        }
        t.cursorRow = len(newLines) - 1
        t.hardwareCursorRow = t.cursorRow
        t.maxLinesRendered = len(newLines)
    } else {
        // Differential render
        firstChanged := -1
        lastChanged := -1
        maxLen := max(len(newLines), len(t.previousLines))

        for i := 0; i < maxLen; i++ {
            var newLine, oldLine string
            if i < len(newLines) {
                newLine = newLines[i]
            }
            if i < len(t.previousLines) {
                oldLine = t.previousLines[i]
            }
            if newLine != oldLine {
                if firstChanged == -1 {
                    firstChanged = i
                }
                lastChanged = i
            }
        }

        if firstChanged == -1 {
            // No changes, just reposition cursor
        } else {
            // Move cursor to firstChanged
            delta := firstChanged - t.hardwareCursorRow
            if delta > 0 {
                fmt.Fprintf(&output, "\x1b[%dB", delta) // move down
            } else if delta < 0 {
                fmt.Fprintf(&output, "\x1b[%dA", -delta) // move up
            }
            output.WriteString("\r") // carriage return to col 0

            // Write changed lines
            for i := firstChanged; i <= lastChanged; i++ {
                output.WriteString("\x1b[2K") // clear line
                if i < len(newLines) {
                    output.WriteString(newLines[i])
                }
                if i < lastChanged {
                    output.WriteString("\r\n")
                }
            }

            // Clear extra lines if old content was longer
            if len(t.previousLines) > len(newLines) {
                for i := len(newLines); i < len(t.previousLines); i++ {
                    output.WriteString("\r\n\x1b[2K")
                }
                // Move back up
                extra := len(t.previousLines) - len(newLines)
                if extra > 0 {
                    fmt.Fprintf(&output, "\x1b[%dA", extra)
                }
            }

            t.hardwareCursorRow = lastChanged
            t.cursorRow = len(newLines) - 1
            if len(newLines) > t.maxLinesRendered {
                t.maxLinesRendered = len(newLines)
            }
        }
    }

    // Position hardware cursor
    if cursorRow >= 0 && t.showCursor {
        delta := cursorRow - t.hardwareCursorRow
        if delta > 0 {
            fmt.Fprintf(&output, "\x1b[%dB", delta)
        } else if delta < 0 {
            fmt.Fprintf(&output, "\x1b[%dA", -delta)
        }
        fmt.Fprintf(&output, "\x1b[%dG", cursorCol+1) // 1-indexed
        t.hardwareCursorRow = cursorRow
        output.WriteString("\x1b[?25h") // show cursor
    } else {
        output.WriteString("\x1b[?25l") // hide cursor
    }

    // Synchronized output end
    output.WriteString("\x1b[?2026l")

    // Write to terminal
    t.terminal.Write([]byte(output.String()))

    // Save state
    t.previousLines = newLines
    t.previousWidth = width
    t.previousHeight = height
    t.renderRequested = false
}
```

### Width overflow guard

```go
// After rendering, verify no line exceeds terminal width.
for _, line := range newLines {
    if VisibleWidth(line) > width {
        // Write crash log, stop TUI, panic
        writeCrashLog(newLines, width)
        t.Stop()
        panic("TUI line exceeds terminal width")
    }
}
```

### Overlay compositing

```go
func (t *TUI) compositeOverlays(baseLines []string, width, height int) []string {
    // Sort overlays by order (lowest first, highest on top)
    sorted := sortOverlaysByOrder(t.overlays)

    for _, overlay := range sorted {
        if overlay.hidden {
            continue
        }
        // Resolve layout
        layout := resolveOverlayLayout(overlay.options, width, height)

        // Render overlay component
        overlayLines := overlay.component.Render(layout.width)

        // Truncate to maxHeight
        if layout.maxHeight > 0 && len(overlayLines) > layout.maxHeight {
            overlayLines = overlayLines[:layout.maxHeight]
        }

        // Pad base to accommodate overlay position
        for len(baseLines) < layout.row+len(overlayLines) {
            baseLines = append(baseLines, "")
        }

        // Composite each overlay line into the base
        for i, overlayLine := range overlayLines {
            row := layout.row + i
            baseLines[row] = compositeLineAt(baseLines[row], overlayLine, layout.col, width)
        }
    }

    return baseLines
}

// compositeLineAt splices overlayLine into baseLine at column col.
func compositeLineAt(baseLine, overlayLine string, col, totalWidth int) string {
    before := SliceByColumn(baseLine, 0, col)
    before = PadToWidth(before, col)
    overlayWidth := VisibleWidth(overlayLine)
    after := SliceByColumn(baseLine, col+overlayWidth, totalWidth)
    return before + "\x1b[0m" + overlayLine + "\x1b[0m" + after
}
```

## 6.8 Components

### Text (`pkg/tui/components/text.go`)

```go
type Text struct {
    text    string
    style   func(string) string // ANSI styling function
    wrap    bool
    cached  []string
}

func NewText(text string) *Text
func (t *Text) SetText(text string)
func (t *Text) SetStyle(style func(string) string)
func (t *Text) Render(width int) []string
func (t *Text) Invalidate()
```

### Editor (`pkg/tui/components/editor.go`)

The most complex component. Multi-line text editor with word wrapping, cursor movement, kill ring, and undo.

```go
type Editor struct {
    lines      []string
    cursorLine int
    cursorCol  int // byte offset within line
    focused    bool

    scrollOffset    int
    maxVisibleLines int

    killRing  *KillRing
    undoStack *UndoStack
    history   []string
    historyIdx int // -1 = not browsing

    keybindings *KeybindingsManager

    OnSubmit func(text string)
    OnChange func(text string)
}

func NewEditor(keybindings *KeybindingsManager) *Editor

func (e *Editor) SetText(text string)
func (e *Editor) Text() string
func (e *Editor) Render(width int) []string
func (e *Editor) HandleInput(data []byte)
func (e *Editor) SetFocused(focused bool)
func (e *Editor) IsFocused() bool
func (e *Editor) Invalidate()
```

Key behaviors:
- Word wrapping via `wordWrapLine`: split at word boundaries, fall back to character-level for oversized words
- Cursor rendered as reverse-video character
- `CursorMarker` emitted at cursor position when focused
- Scroll offset keeps cursor visible within `maxVisibleLines` (30% of terminal height, min 5)
- Border indicators: `↑ N more` / `↓ N more` for scrolled content
- Kill ring: Emacs-style kill/yank with accumulation for consecutive kills

### Markdown (`pkg/tui/components/markdown.go`)

```go
type Markdown struct {
    text   string
    cached []string
}

func NewMarkdown(text string) *Markdown
func (m *Markdown) SetText(text string)
func (m *Markdown) Render(width int) []string
func (m *Markdown) Invalidate()
```

Implementation approach:
- Use a simple markdown lexer (can use `github.com/yuin/goldmark` for parsing, then custom ANSI renderer)
- Or implement a minimal lexer for: headers, code blocks, inline code, bold, italic, lists, links, blockquotes, horizontal rules
- Code block syntax highlighting via `github.com/alecthomas/chroma/v2`
- Word wrapping with ANSI-awareness

### SelectList (`pkg/tui/components/select_list.go`)

```go
type SelectItem struct {
    Label       string
    Description string
    Value       any
}

type SelectList struct {
    items      []SelectItem
    filtered   []int // indices into items
    filter     string
    selected   int   // index into filtered
    focused    bool
    maxVisible int

    OnConfirm func(item SelectItem)
    OnCancel  func()
}

func NewSelectList(items []SelectItem) *SelectList
func (s *SelectList) Render(width int) []string
func (s *SelectList) HandleInput(data []byte)
```

Fuzzy filtering: match filter string against item labels (case-insensitive substring match, or implement a simple fuzzy scorer).

### Box, Loader, Spacer

Simple components:

```go
// Box wraps a component with a border
type Box struct {
    child  Component
    title  string
    style  BoxStyle // single, double, rounded, etc.
}

// Loader shows a spinning animation, liveness-aware.
// Displays different animations and text depending on the agent status.
//
// When thinking:  standard spinner with elapsed time.
//   e.g. "⠋ Thinking... 5s"
//
// When executing: spinner showing the active tool name and elapsed time.
//   e.g. "⠋ Running bash... 12s"
//
// When stalled:   color shifts to yellow (>30s) or red (>60s) with a
//                 warning icon replacing the spinner.
//   e.g. "⚠ bash still running... 65s"
//
// When idle: the loader is hidden (renders zero lines).
type Loader struct {
    status    string        // "thinking", "executing", "stalled", "idle"
    toolName  string        // active tool name, set during executing/stalled
    elapsed   time.Duration // time in current state
    frame     int
    ticker    *time.Ticker
}

func (l *Loader) Render(width int) []string {
    // Returns zero lines when idle.
    // Selects spinner frame, prefix icon, label, and ANSI color
    // based on l.status and l.elapsed.
}

// StatusBar shows model info, agent status, token usage, and cost.
// Color-coded status indicator:
//   green  = thinking
//   blue   = executing
//   yellow = stalled (warning)
//   red    = stalled (critical, >60s)
//   dim    = idle
type StatusBar struct {
    model         string
    thinkingLevel string
    status        string        // current liveness state
    elapsed       time.Duration
    tokenCount    int
    cost          float64
}

func NewStatusBar() *StatusBar
func (s *StatusBar) Render(width int) []string
func (s *StatusBar) Invalidate()

// Spacer renders N empty lines
type Spacer struct {
    lines int
}
```

## 6.9 Kill ring and undo stack

```go
type KillRing struct {
    entries []string
    index   int
    maxSize int
}

func NewKillRing(maxSize int) *KillRing
func (k *KillRing) Push(text string, prepend bool) // prepend for backward kills
func (k *KillRing) Peek() string
func (k *KillRing) Rotate() string // cycle to next entry

type UndoStack struct {
    stack []EditorSnapshot
}

type EditorSnapshot struct {
    Lines     []string
    CursorLine int
    CursorCol  int
}

func NewUndoStack() *UndoStack
func (u *UndoStack) Push(snapshot EditorSnapshot)
func (u *UndoStack) Pop() (EditorSnapshot, bool)
```

## 6.10 Virtual terminal for testing

```go
type VirtualTerminal struct {
    width   int
    height  int
    output  []string
    inputCh chan []byte
}

func NewVirtualTerminal(width, height int) *VirtualTerminal
func (v *VirtualTerminal) SimulateInput(data []byte)
func (v *VirtualTerminal) LastOutput() []string

Liveness rendering can be tested by feeding a sequence of `LivenessEvent` values into
the TUI (via the same channel the real agent uses), then inspecting the rendered output
from `LastOutput()`. Verify that the output contains the expected status text, tool name,
elapsed time formatting, and ANSI color codes for each state transition
(idle → thinking → executing → stalled → idle).
```

## 6.11 Tests

- `width_test.go`: VisibleWidth with plain text, ANSI codes, wide chars, mixed
- `keys_test.go`: MatchesKey for ctrl+c, shift+enter, arrows, Kitty sequences, legacy sequences
- `tui_test.go`: Differential rendering with VirtualTerminal: initial render, partial update, shrink, grow
- `editor_test.go`: Insert text, cursor movement, word wrap, delete, kill/yank, undo, scroll
- `select_list_test.go`: Filtering, selection, confirm/cancel
- `container_test.go`: Add/remove children, render concatenation

### Verification criteria

- [ ] `go test ./pkg/tui/...` passes
- [ ] Differential renderer only writes changed lines (verify by counting write calls)
- [ ] Editor handles multi-line editing with correct cursor tracking
- [ ] Key parsing handles both Kitty and legacy sequences
- [ ] Overlay compositing correctly splices content at column positions
- [ ] Width calculation handles ANSI codes and wide characters
