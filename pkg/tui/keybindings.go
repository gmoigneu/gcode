package tui

import "sync"

// KeybindingID is a stable identifier for a logical action (e.g.
// "tui.editor.cursorUp"). Actions are decoupled from the concrete keys
// that trigger them so users can remap without touching component code.
type KeybindingID string

// Default keybinding IDs used by the built-in components.
const (
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

// KeybindingDef describes a logical action: its default key combinations
// and a human-readable description.
type KeybindingDef struct {
	DefaultKeys []KeyID
	Description string
}

// DefaultKeybindings is the factory shipping set of bindings. Callers
// should not mutate this map directly; use KeybindingsManager.Override.
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
	KBUndo:              {DefaultKeys: []KeyID{"ctrl+_"}, Description: "Undo"},
	KBNewLine:           {DefaultKeys: []KeyID{"shift+enter", "alt+enter"}, Description: "Insert newline"},
	KBSubmit:            {DefaultKeys: []KeyID{KeyEnter}, Description: "Submit"},
	KBSelectUp:          {DefaultKeys: []KeyID{KeyUp, "ctrl+p"}, Description: "Select previous"},
	KBSelectDown:        {DefaultKeys: []KeyID{KeyDown, "ctrl+n"}, Description: "Select next"},
	KBSelectConfirm:     {DefaultKeys: []KeyID{KeyEnter}, Description: "Confirm selection"},
	KBSelectCancel:      {DefaultKeys: []KeyID{KeyEscape, "ctrl+c"}, Description: "Cancel selection"},
}

// KeybindingsManager resolves KeybindingIDs to concrete KeyIDs. User
// overrides replace the defaults per action.
type KeybindingsManager struct {
	mu            sync.RWMutex
	definitions   map[KeybindingID]KeybindingDef
	userOverrides map[KeybindingID][]KeyID
}

// NewKeybindingsManager returns a manager seeded with DefaultKeybindings.
func NewKeybindingsManager() *KeybindingsManager {
	defs := make(map[KeybindingID]KeybindingDef, len(DefaultKeybindings))
	for k, v := range DefaultKeybindings {
		defs[k] = KeybindingDef{
			DefaultKeys: append([]KeyID(nil), v.DefaultKeys...),
			Description: v.Description,
		}
	}
	return &KeybindingsManager{
		definitions:   defs,
		userOverrides: make(map[KeybindingID][]KeyID),
	}
}

// Override replaces the key list for an action. Pass an empty slice to
// unbind the action entirely.
func (m *KeybindingsManager) Override(id KeybindingID, keys []KeyID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if keys == nil {
		delete(m.userOverrides, id)
		return
	}
	m.userOverrides[id] = append([]KeyID(nil), keys...)
}

// Keys returns the effective key list for id (overrides then defaults).
func (m *KeybindingsManager) Keys(id KeybindingID) []KeyID {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if override, ok := m.userOverrides[id]; ok {
		return override
	}
	if def, ok := m.definitions[id]; ok {
		return def.DefaultKeys
	}
	return nil
}

// Matches reports whether the raw terminal input matches any key bound to
// the given action.
func (m *KeybindingsManager) Matches(data []byte, id KeybindingID) bool {
	parsed, ok := ParseKey(data)
	if !ok {
		return false
	}
	for _, k := range m.Keys(id) {
		if k == parsed {
			return true
		}
	}
	return false
}

// Definitions returns the map of all known actions. The returned map is a
// snapshot; mutating it does not affect the manager.
func (m *KeybindingsManager) Definitions() map[KeybindingID]KeybindingDef {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[KeybindingID]KeybindingDef, len(m.definitions))
	for k, v := range m.definitions {
		out[k] = v
	}
	return out
}
