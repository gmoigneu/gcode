package tui

import (
	"fmt"
	"strconv"
	"strings"
)

// KeyID identifies a keyboard key or combination. Bare keys use short
// names ("up", "enter", "a"), modifier combinations are hyphenated with a
// "+" separator ("ctrl+c", "alt+shift+f").
type KeyID string

// Bare key constants.
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
	KeyInsert    KeyID = "insert"
)

// legacySequences maps raw escape sequences to KeyIDs. The table is
// intentionally exhaustive for the sequences pi relied on and is consulted
// before Kitty CSI-u parsing.
var legacySequences = map[string]KeyID{
	"\x1b[A":    KeyUp,
	"\x1b[B":    KeyDown,
	"\x1b[C":    KeyRight,
	"\x1b[D":    KeyLeft,
	"\x1bOA":    KeyUp,
	"\x1bOB":    KeyDown,
	"\x1bOC":    KeyRight,
	"\x1bOD":    KeyLeft,
	"\x1b[H":    KeyHome,
	"\x1b[F":    KeyEnd,
	"\x1bOH":    KeyHome,
	"\x1bOF":    KeyEnd,
	"\x1b[1~":   KeyHome,
	"\x1b[4~":   KeyEnd,
	"\x1b[2~":   KeyInsert,
	"\x1b[3~":   KeyDelete,
	"\x1b[5~":   KeyPageUp,
	"\x1b[6~":   KeyPageDown,
	"\x1b[Z":    "shift+tab",
	"\x1b[1;2A": "shift+up",
	"\x1b[1;2B": "shift+down",
	"\x1b[1;2C": "shift+right",
	"\x1b[1;2D": "shift+left",
	"\x1b[1;3A": "alt+up",
	"\x1b[1;3B": "alt+down",
	"\x1b[1;3C": "alt+right",
	"\x1b[1;3D": "alt+left",
	"\x1b[1;5A": "ctrl+up",
	"\x1b[1;5B": "ctrl+down",
	"\x1b[1;5C": "ctrl+right",
	"\x1b[1;5D": "ctrl+left",
}

// ParseKey converts a raw input byte sequence into a KeyID. Returns
// false if the sequence cannot be classified.
func ParseKey(data []byte) (KeyID, bool) {
	if len(data) == 0 {
		return "", false
	}

	// Single-byte cases.
	if len(data) == 1 {
		b := data[0]
		switch b {
		case 0x1B:
			return KeyEscape, true
		case '\r', '\n':
			return KeyEnter, true
		case '\t':
			return KeyTab, true
		case 0x7F, 0x08:
			return KeyBackspace, true
		case ' ':
			return KeySpace, true
		}
		// Ctrl+letter is the byte with bits 0-4 set (0x01 = ctrl+a).
		if b >= 0x01 && b <= 0x1A && b != '\r' && b != '\n' && b != '\t' {
			return KeyID(fmt.Sprintf("ctrl+%c", 'a'+b-1)), true
		}
		// Ctrl+punctuation that terminals map to the 0x1C..0x1F range.
		switch b {
		case 0x1C:
			return "ctrl+\\", true
		case 0x1D:
			return "ctrl+]", true
		case 0x1E:
			return "ctrl+^", true
		case 0x1F:
			return "ctrl+_", true
		}
		if b >= 0x20 && b < 0x7F {
			return KeyID(string(b)), true
		}
	}

	// Legacy tables.
	if k, ok := legacySequences[string(data)]; ok {
		return k, true
	}

	// ESC + ASCII = alt+ASCII (meta key).
	if len(data) == 2 && data[0] == 0x1B {
		if data[1] >= 0x20 && data[1] < 0x7F {
			return KeyID("alt+" + string(data[1])), true
		}
		if data[1] == 0x7F || data[1] == 0x08 {
			return "alt+backspace", true
		}
	}

	// Kitty CSI-u: ESC [ codepoint ; modifiers u
	if id, ok := parseKittyCSIu(data); ok {
		return id, true
	}

	return "", false
}

// MatchesKey is a convenience wrapper around ParseKey for components that
// want to test an incoming sequence against a specific key ID.
func MatchesKey(data []byte, key KeyID) bool {
	got, ok := ParseKey(data)
	return ok && got == key
}

// IsKeyRelease reports whether the sequence is a Kitty key-release event.
// Legacy terminals don't emit these, so the default is false.
func IsKeyRelease(data []byte) bool {
	_, _, eventType, ok := parseKittyCSIuFields(data)
	return ok && eventType == 3
}

// parseKittyCSIu parses CSI-u key events: ESC [ codepoint ; modifiers[:event] u
// Returns the KeyID or false if not a CSI-u sequence.
func parseKittyCSIu(data []byte) (KeyID, bool) {
	cp, mods, _, ok := parseKittyCSIuFields(data)
	if !ok {
		return "", false
	}
	base := baseKeyForCodepoint(cp)
	if base == "" {
		return "", false
	}
	return keyWithMods(base, mods), true
}

func parseKittyCSIuFields(data []byte) (codepoint, modifiers, eventType int, ok bool) {
	if len(data) < 4 || data[0] != 0x1B || data[1] != '[' {
		return 0, 0, 0, false
	}
	if data[len(data)-1] != 'u' {
		return 0, 0, 0, false
	}
	body := string(data[2 : len(data)-1])

	// Split on ';' — first segment is the codepoint, second is modifiers
	// and event type joined with ':'.
	parts := strings.Split(body, ";")
	if len(parts) == 0 {
		return 0, 0, 0, false
	}
	cp, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, 0, false
	}
	mods := 1
	et := 1
	if len(parts) > 1 {
		modParts := strings.Split(parts[1], ":")
		if v, err := strconv.Atoi(modParts[0]); err == nil {
			mods = v
		}
		if len(modParts) > 1 {
			if v, err := strconv.Atoi(modParts[1]); err == nil {
				et = v
			}
		}
	}
	return cp, mods, et, true
}

// baseKeyForCodepoint maps a codepoint to a KeyID base. Only printable
// ASCII and a handful of named keys are supported for now.
func baseKeyForCodepoint(cp int) KeyID {
	if cp >= 0x20 && cp < 0x7F {
		return KeyID(string(rune(cp)))
	}
	switch cp {
	case 13:
		return KeyEnter
	case 9:
		return KeyTab
	case 27:
		return KeyEscape
	case 127:
		return KeyBackspace
	}
	return ""
}

// keyWithMods combines a base key with a modifier bitmask (Kitty-style,
// 1-indexed: actual = mods - 1, bits: shift=1, alt=2, ctrl=4).
func keyWithMods(base KeyID, mods int) KeyID {
	actual := mods - 1
	if actual <= 0 {
		return base
	}
	var parts []string
	if actual&4 != 0 {
		parts = append(parts, "ctrl")
	}
	if actual&2 != 0 {
		parts = append(parts, "alt")
	}
	if actual&1 != 0 {
		parts = append(parts, "shift")
	}
	parts = append(parts, string(base))
	return KeyID(strings.Join(parts, "+"))
}

// CtrlChar returns the raw byte a terminal sends for ctrl+<letter>.
func CtrlChar(letter byte) byte {
	switch {
	case letter >= 'a' && letter <= 'z':
		return letter - 'a' + 1
	case letter >= 'A' && letter <= 'Z':
		return letter - 'A' + 1
	}
	return 0
}
