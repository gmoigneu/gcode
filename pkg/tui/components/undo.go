package components

// EditorSnapshot captures the editor's state so it can be restored on
// undo.
type EditorSnapshot struct {
	Lines      []string
	CursorLine int
	CursorCol  int
}

// UndoStack is a fixed-capacity LIFO of editor snapshots.
type UndoStack struct {
	stack   []EditorSnapshot
	maxSize int
}

// NewUndoStack creates an UndoStack with the given capacity. Zero or
// negative capacity defaults to 100.
func NewUndoStack(maxSize int) *UndoStack {
	if maxSize <= 0 {
		maxSize = 100
	}
	return &UndoStack{maxSize: maxSize}
}

// Push records a new snapshot. Older snapshots are dropped when the
// capacity is exceeded.
func (u *UndoStack) Push(s EditorSnapshot) {
	u.stack = append(u.stack, s)
	if len(u.stack) > u.maxSize {
		u.stack = u.stack[1:]
	}
}

// Pop removes and returns the most recent snapshot. Returns ok=false
// when the stack is empty.
func (u *UndoStack) Pop() (EditorSnapshot, bool) {
	if len(u.stack) == 0 {
		return EditorSnapshot{}, false
	}
	top := u.stack[len(u.stack)-1]
	u.stack = u.stack[:len(u.stack)-1]
	return top, true
}

// Len returns the number of snapshots currently held.
func (u *UndoStack) Len() int { return len(u.stack) }
