package components

import (
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/gmoigneu/gcode/pkg/tui"
)

// Editor is a focusable multi-line text input with word wrapping, a
// kill ring, and an undo stack. The internal representation is a slice
// of logical lines (no newlines); rendering wraps them to the available
// width.
type Editor struct {
	mu sync.RWMutex

	lines      []string
	cursorLine int
	cursorCol  int // byte offset inside lines[cursorLine]
	focused    bool

	killRing    *KillRing
	undoStack   *UndoStack
	keybindings *tui.KeybindingsManager

	// Track the last input category so we can group consecutive
	// kills / inserts for undo coalescing.
	lastAction string

	OnSubmit func(text string)
	OnChange func(text string)
}

// NewEditor creates an empty editor bound to the given keybindings.
// Nil keybindings fall back to the default manager.
func NewEditor(kb *tui.KeybindingsManager) *Editor {
	if kb == nil {
		kb = tui.NewKeybindingsManager()
	}
	return &Editor{
		lines:       []string{""},
		killRing:    NewKillRing(32),
		undoStack:   NewUndoStack(100),
		keybindings: kb,
	}
}

// ----- basic accessors -----

// Text returns the full editor contents joined by newlines.
func (e *Editor) Text() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return strings.Join(e.lines, "\n")
}

// SetText replaces the editor contents. The cursor is placed at end.
func (e *Editor) SetText(text string) {
	e.mu.Lock()
	e.lines = strings.Split(text, "\n")
	if len(e.lines) == 0 {
		e.lines = []string{""}
	}
	e.cursorLine = len(e.lines) - 1
	e.cursorCol = len(e.lines[e.cursorLine])
	e.mu.Unlock()
	e.fireChange()
}

// Clear empties the editor.
func (e *Editor) Clear() { e.SetText("") }

// CursorPosition returns the current (line, col) cursor location.
func (e *Editor) CursorPosition() (int, int) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.cursorLine, e.cursorCol
}

// SetFocused marks the editor focused.
func (e *Editor) SetFocused(focused bool) {
	e.mu.Lock()
	e.focused = focused
	e.mu.Unlock()
}

// IsFocused reports whether the editor currently has focus.
func (e *Editor) IsFocused() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.focused
}

// ----- rendering -----

// Render word-wraps the logical lines to fit the given width. When the
// editor is focused, a CursorMarker is injected at the cursor position
// so the TUI can place the hardware cursor there.
func (e *Editor) Render(width int) []string {
	if width <= 0 {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()

	var out []string
	for li, line := range e.lines {
		wrapped := wordWrap(line, width)
		if len(wrapped) == 0 {
			wrapped = []string{""}
		}
		for wi, wline := range wrapped {
			if e.focused && li == e.cursorLine && wi == e.cursorWrapIndex(line, width) {
				wline = insertCursorMarker(wline, e.cursorColWithin(line, width))
			}
			out = append(out, wline)
		}
	}
	return out
}

// Invalidate is a no-op (editor state is the source of truth).
func (e *Editor) Invalidate() {}

// cursorWrapIndex returns the index of the wrapped line the cursor
// sits on given the logical line and width. Uses byte offsets.
func (e *Editor) cursorWrapIndex(line string, width int) int {
	if e.cursorCol <= 0 {
		return 0
	}
	col := 0
	wrapped := wordWrap(line, width)
	consumed := 0
	for i, wline := range wrapped {
		lw := len(wline)
		if e.cursorCol >= consumed && e.cursorCol <= consumed+lw {
			return i
		}
		consumed += lw
		col += tui.VisibleWidth(wline)
	}
	_ = col
	return len(wrapped) - 1
}

// cursorColWithin returns the column offset (in visible columns) of the
// cursor within its wrapped line.
func (e *Editor) cursorColWithin(line string, width int) int {
	wrapped := wordWrap(line, width)
	consumed := 0
	for _, wline := range wrapped {
		lw := len(wline)
		if e.cursorCol >= consumed && e.cursorCol <= consumed+lw {
			sub := wline[:e.cursorCol-consumed]
			return tui.VisibleWidth(sub)
		}
		consumed += lw
	}
	return 0
}

// insertCursorMarker splices the CursorMarker into line at the given
// visible column. The line is not byte-indexed — we walk runes and
// count their widths.
func insertCursorMarker(line string, col int) string {
	if col <= 0 {
		return tui.CursorMarker + line
	}
	var b strings.Builder
	acc := 0
	inserted := false
	for _, r := range line {
		if !inserted && acc >= col {
			b.WriteString(tui.CursorMarker)
			inserted = true
		}
		b.WriteRune(r)
		acc += runeVisibleWidth(r)
	}
	if !inserted {
		b.WriteString(tui.CursorMarker)
	}
	return b.String()
}

func runeVisibleWidth(r rune) int {
	return tui.VisibleWidth(string(r))
}

// wordWrap breaks a logical line into display-width-sized chunks,
// preferring word boundaries but falling back to hard breaks for
// oversized words.
func wordWrap(line string, width int) []string {
	if width <= 0 {
		return []string{line}
	}
	if tui.VisibleWidth(line) <= width {
		return []string{line}
	}

	var out []string
	words := splitWords(line)
	var cur strings.Builder
	curWidth := 0

	for _, word := range words {
		ww := tui.VisibleWidth(word)
		if ww > width {
			if curWidth > 0 {
				out = append(out, cur.String())
				cur.Reset()
				curWidth = 0
			}
			// Hard-break the long word.
			chunks := hardWrap(word, width)
			out = append(out, chunks[:len(chunks)-1]...)
			cur.WriteString(chunks[len(chunks)-1])
			curWidth = tui.VisibleWidth(chunks[len(chunks)-1])
			continue
		}
		if curWidth+ww > width && curWidth > 0 {
			out = append(out, cur.String())
			cur.Reset()
			curWidth = 0
		}
		cur.WriteString(word)
		curWidth += ww
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	if len(out) == 0 {
		out = []string{""}
	}
	return out
}

// splitWords splits on whitespace while preserving the separators so
// wordWrap can re-concatenate without losing spacing.
func splitWords(line string) []string {
	if line == "" {
		return nil
	}
	var out []string
	start := 0
	inWord := !unicode.IsSpace(rune(line[0]))
	for i, r := range line {
		if unicode.IsSpace(r) == !inWord {
			continue
		}
		if i > start {
			out = append(out, line[start:i])
			start = i
		}
		inWord = !unicode.IsSpace(r)
	}
	if start < len(line) {
		out = append(out, line[start:])
	}
	return out
}

// hardWrap chops a string into fixed-width chunks at column boundaries.
func hardWrap(s string, width int) []string {
	var out []string
	curWidth := 0
	var cur strings.Builder
	for _, r := range s {
		rw := runeVisibleWidth(r)
		if curWidth+rw > width {
			out = append(out, cur.String())
			cur.Reset()
			curWidth = 0
		}
		cur.WriteRune(r)
		curWidth += rw
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// ----- input handling -----

// HandleInput processes a single key event. Unknown input is inserted
// as text.
func (e *Editor) HandleInput(data []byte) {
	if !e.IsFocused() {
		return
	}
	kb := e.keybindings

	// Cursor movement.
	switch {
	case kb.Matches(data, tui.KBCursorLeft):
		e.moveCursorLeft()
	case kb.Matches(data, tui.KBCursorRight):
		e.moveCursorRight()
	case kb.Matches(data, tui.KBCursorUp):
		e.moveCursorUp()
	case kb.Matches(data, tui.KBCursorDown):
		e.moveCursorDown()
	case kb.Matches(data, tui.KBCursorWordLeft):
		e.moveCursorWordLeft()
	case kb.Matches(data, tui.KBCursorWordRight):
		e.moveCursorWordRight()
	case kb.Matches(data, tui.KBCursorLineStart):
		e.moveCursorLineStart()
	case kb.Matches(data, tui.KBCursorLineEnd):
		e.moveCursorLineEnd()

	// Deletion.
	case kb.Matches(data, tui.KBDeleteCharBack):
		e.pushUndo()
		e.deleteCharBack()
		e.fireChange()
	case kb.Matches(data, tui.KBDeleteCharFwd):
		e.pushUndo()
		e.deleteCharFwd()
		e.fireChange()
	case kb.Matches(data, tui.KBDeleteWordBack):
		e.pushUndo()
		text := e.deleteWordBack()
		if text != "" {
			e.killRing.PushNew(text)
		}
		e.fireChange()
	case kb.Matches(data, tui.KBDeleteWordFwd):
		e.pushUndo()
		text := e.deleteWordFwd()
		if text != "" {
			e.killRing.PushNew(text)
		}
		e.fireChange()
	case kb.Matches(data, tui.KBDeleteToLineStart):
		e.pushUndo()
		text := e.deleteToLineStart()
		if text != "" {
			e.killRing.PushNew(text)
		}
		e.fireChange()
	case kb.Matches(data, tui.KBDeleteToLineEnd):
		e.pushUndo()
		text := e.deleteToLineEnd()
		if text != "" {
			e.killRing.PushNew(text)
		}
		e.fireChange()

	// Kill-ring yank / pop.
	case kb.Matches(data, tui.KBYank):
		e.pushUndo()
		e.insertString(e.killRing.Peek())
		e.fireChange()
	case kb.Matches(data, tui.KBYankPop):
		if e.lastAction == "yank" {
			e.undo()
			e.insertString(e.killRing.Rotate())
			e.fireChange()
		}

	// Undo.
	case kb.Matches(data, tui.KBUndo):
		e.undo()
		e.fireChange()

	// Newline / submit.
	case kb.Matches(data, tui.KBNewLine):
		e.pushUndo()
		e.insertNewline()
		e.fireChange()
	case kb.Matches(data, tui.KBSubmit):
		if e.OnSubmit != nil {
			e.OnSubmit(e.Text())
		}

	default:
		// Fall through to insertion for printable input.
		e.insertRawBytes(data)
	}

	if kb.Matches(data, tui.KBYank) {
		e.lastAction = "yank"
	} else {
		e.lastAction = ""
	}
}

func (e *Editor) insertRawBytes(data []byte) {
	if len(data) == 0 {
		return
	}
	// Reject control characters other than printable UTF-8 runes.
	if len(data) == 1 && data[0] < 0x20 && data[0] != 0x1B && data[0] != 0x09 && data[0] != 0x0D && data[0] != 0x0A {
		return
	}
	if !utf8.Valid(data) {
		return
	}
	e.pushUndo()
	e.insertString(string(data))
	e.fireChange()
}

func (e *Editor) insertString(s string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if s == "" {
		return
	}
	if !strings.Contains(s, "\n") {
		line := e.lines[e.cursorLine]
		e.lines[e.cursorLine] = line[:e.cursorCol] + s + line[e.cursorCol:]
		e.cursorCol += len(s)
		return
	}
	// Handle multi-line paste by splitting on \n.
	parts := strings.Split(s, "\n")
	current := e.lines[e.cursorLine]
	before := current[:e.cursorCol]
	after := current[e.cursorCol:]

	newLines := make([]string, 0, len(e.lines)+len(parts)-1)
	newLines = append(newLines, e.lines[:e.cursorLine]...)
	newLines = append(newLines, before+parts[0])
	for _, p := range parts[1 : len(parts)-1] {
		newLines = append(newLines, p)
	}
	last := parts[len(parts)-1]
	newLines = append(newLines, last+after)
	newLines = append(newLines, e.lines[e.cursorLine+1:]...)

	e.lines = newLines
	e.cursorLine += len(parts) - 1
	e.cursorCol = len(last)
}

func (e *Editor) insertNewline() {
	e.mu.Lock()
	defer e.mu.Unlock()
	line := e.lines[e.cursorLine]
	before := line[:e.cursorCol]
	after := line[e.cursorCol:]
	e.lines[e.cursorLine] = before
	rest := append([]string{after}, e.lines[e.cursorLine+1:]...)
	e.lines = append(e.lines[:e.cursorLine+1], rest...)
	e.cursorLine++
	e.cursorCol = 0
}

// ----- cursor movement -----

func (e *Editor) moveCursorLeft() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cursorCol > 0 {
		_, size := utf8.DecodeLastRuneInString(e.lines[e.cursorLine][:e.cursorCol])
		e.cursorCol -= size
		return
	}
	if e.cursorLine > 0 {
		e.cursorLine--
		e.cursorCol = len(e.lines[e.cursorLine])
	}
}

func (e *Editor) moveCursorRight() {
	e.mu.Lock()
	defer e.mu.Unlock()
	line := e.lines[e.cursorLine]
	if e.cursorCol < len(line) {
		_, size := utf8.DecodeRuneInString(line[e.cursorCol:])
		e.cursorCol += size
		return
	}
	if e.cursorLine < len(e.lines)-1 {
		e.cursorLine++
		e.cursorCol = 0
	}
}

func (e *Editor) moveCursorUp() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cursorLine > 0 {
		e.cursorLine--
		if e.cursorCol > len(e.lines[e.cursorLine]) {
			e.cursorCol = len(e.lines[e.cursorLine])
		}
	}
}

func (e *Editor) moveCursorDown() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cursorLine < len(e.lines)-1 {
		e.cursorLine++
		if e.cursorCol > len(e.lines[e.cursorLine]) {
			e.cursorCol = len(e.lines[e.cursorLine])
		}
	}
}

func (e *Editor) moveCursorLineStart() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cursorCol = 0
}

func (e *Editor) moveCursorLineEnd() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cursorCol = len(e.lines[e.cursorLine])
}

func (e *Editor) moveCursorWordLeft() {
	e.mu.Lock()
	defer e.mu.Unlock()
	line := e.lines[e.cursorLine]
	pos := e.cursorCol
	// Skip any whitespace left of the cursor.
	for pos > 0 && unicode.IsSpace(rune(line[pos-1])) {
		pos--
	}
	// Walk back until another whitespace or start.
	for pos > 0 && !unicode.IsSpace(rune(line[pos-1])) {
		pos--
	}
	e.cursorCol = pos
}

func (e *Editor) moveCursorWordRight() {
	e.mu.Lock()
	defer e.mu.Unlock()
	line := e.lines[e.cursorLine]
	pos := e.cursorCol
	for pos < len(line) && unicode.IsSpace(rune(line[pos])) {
		pos++
	}
	for pos < len(line) && !unicode.IsSpace(rune(line[pos])) {
		pos++
	}
	e.cursorCol = pos
}

// ----- deletion helpers -----

func (e *Editor) deleteCharBack() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cursorCol > 0 {
		line := e.lines[e.cursorLine]
		_, size := utf8.DecodeLastRuneInString(line[:e.cursorCol])
		e.lines[e.cursorLine] = line[:e.cursorCol-size] + line[e.cursorCol:]
		e.cursorCol -= size
		return
	}
	if e.cursorLine > 0 {
		prev := e.lines[e.cursorLine-1]
		cur := e.lines[e.cursorLine]
		e.lines = append(e.lines[:e.cursorLine-1], append([]string{prev + cur}, e.lines[e.cursorLine+1:]...)...)
		e.cursorLine--
		e.cursorCol = len(prev)
	}
}

func (e *Editor) deleteCharFwd() {
	e.mu.Lock()
	defer e.mu.Unlock()
	line := e.lines[e.cursorLine]
	if e.cursorCol < len(line) {
		_, size := utf8.DecodeRuneInString(line[e.cursorCol:])
		e.lines[e.cursorLine] = line[:e.cursorCol] + line[e.cursorCol+size:]
		return
	}
	if e.cursorLine < len(e.lines)-1 {
		next := e.lines[e.cursorLine+1]
		e.lines[e.cursorLine] = line + next
		e.lines = append(e.lines[:e.cursorLine+1], e.lines[e.cursorLine+2:]...)
	}
}

func (e *Editor) deleteWordBack() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	line := e.lines[e.cursorLine]
	pos := e.cursorCol
	for pos > 0 && unicode.IsSpace(rune(line[pos-1])) {
		pos--
	}
	for pos > 0 && !unicode.IsSpace(rune(line[pos-1])) {
		pos--
	}
	deleted := line[pos:e.cursorCol]
	e.lines[e.cursorLine] = line[:pos] + line[e.cursorCol:]
	e.cursorCol = pos
	return deleted
}

func (e *Editor) deleteWordFwd() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	line := e.lines[e.cursorLine]
	pos := e.cursorCol
	for pos < len(line) && unicode.IsSpace(rune(line[pos])) {
		pos++
	}
	for pos < len(line) && !unicode.IsSpace(rune(line[pos])) {
		pos++
	}
	deleted := line[e.cursorCol:pos]
	e.lines[e.cursorLine] = line[:e.cursorCol] + line[pos:]
	return deleted
}

func (e *Editor) deleteToLineStart() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	line := e.lines[e.cursorLine]
	deleted := line[:e.cursorCol]
	e.lines[e.cursorLine] = line[e.cursorCol:]
	e.cursorCol = 0
	return deleted
}

func (e *Editor) deleteToLineEnd() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	line := e.lines[e.cursorLine]
	deleted := line[e.cursorCol:]
	e.lines[e.cursorLine] = line[:e.cursorCol]
	return deleted
}

// ----- undo -----

func (e *Editor) pushUndo() {
	e.mu.RLock()
	snap := EditorSnapshot{
		Lines:      append([]string(nil), e.lines...),
		CursorLine: e.cursorLine,
		CursorCol:  e.cursorCol,
	}
	e.mu.RUnlock()
	e.undoStack.Push(snap)
}

func (e *Editor) undo() {
	snap, ok := e.undoStack.Pop()
	if !ok {
		return
	}
	e.mu.Lock()
	e.lines = append([]string(nil), snap.Lines...)
	e.cursorLine = snap.CursorLine
	e.cursorCol = snap.CursorCol
	e.mu.Unlock()
}

// fireChange invokes OnChange if set.
func (e *Editor) fireChange() {
	if e.OnChange != nil {
		e.OnChange(e.Text())
	}
}
