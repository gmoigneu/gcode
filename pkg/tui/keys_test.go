package tui

import "testing"

func TestParseSingleChar(t *testing.T) {
	k, ok := ParseKey([]byte("a"))
	if !ok || k != "a" {
		t.Errorf("got %q", k)
	}
}

func TestParseEnter(t *testing.T) {
	for _, data := range [][]byte{{'\r'}, {'\n'}} {
		k, ok := ParseKey(data)
		if !ok || k != KeyEnter {
			t.Errorf("got %q for %v", k, data)
		}
	}
}

func TestParseBackspace(t *testing.T) {
	k, _ := ParseKey([]byte{0x7F})
	if k != KeyBackspace {
		t.Errorf("got %q", k)
	}
}

func TestParseCtrlLetter(t *testing.T) {
	k, _ := ParseKey([]byte{0x03}) // ctrl+c
	if k != "ctrl+c" {
		t.Errorf("got %q", k)
	}
	k, _ = ParseKey([]byte{0x11}) // ctrl+q
	if k != "ctrl+q" {
		t.Errorf("got %q", k)
	}
}

func TestParseEscape(t *testing.T) {
	k, _ := ParseKey([]byte{0x1B})
	if k != KeyEscape {
		t.Errorf("got %q", k)
	}
}

func TestParseArrowsLegacy(t *testing.T) {
	cases := map[string]KeyID{
		"\x1b[A": KeyUp,
		"\x1b[B": KeyDown,
		"\x1b[C": KeyRight,
		"\x1b[D": KeyLeft,
	}
	for seq, want := range cases {
		k, ok := ParseKey([]byte(seq))
		if !ok || k != want {
			t.Errorf("%q: got %q, want %q", seq, k, want)
		}
	}
}

func TestParseModifiedArrows(t *testing.T) {
	k, _ := ParseKey([]byte("\x1b[1;5A"))
	if k != "ctrl+up" {
		t.Errorf("got %q", k)
	}
	k, _ = ParseKey([]byte("\x1b[1;3D"))
	if k != "alt+left" {
		t.Errorf("got %q", k)
	}
}

func TestParseAltLetter(t *testing.T) {
	k, _ := ParseKey([]byte{0x1B, 'b'})
	if k != "alt+b" {
		t.Errorf("got %q", k)
	}
}

func TestParseAltBackspace(t *testing.T) {
	k, _ := ParseKey([]byte{0x1B, 0x7F})
	if k != "alt+backspace" {
		t.Errorf("got %q", k)
	}
}

func TestParseKittyCSIuBasic(t *testing.T) {
	// 'a' codepoint 97, no modifiers: ESC [ 97 u
	k, ok := ParseKey([]byte("\x1b[97u"))
	if !ok || k != "a" {
		t.Errorf("got %q", k)
	}
}

func TestParseKittyCSIuCtrlC(t *testing.T) {
	// 'c' codepoint 99, ctrl mods = 5 (bit 4 set + 1): ESC [ 99 ; 5 u
	k, ok := ParseKey([]byte("\x1b[99;5u"))
	if !ok || k != "ctrl+c" {
		t.Errorf("got %q", k)
	}
}

func TestParseKittyCSIuShiftEnter(t *testing.T) {
	// Enter codepoint 13, shift mods = 2: ESC [ 13 ; 2 u
	k, ok := ParseKey([]byte("\x1b[13;2u"))
	if !ok || k != "shift+enter" {
		t.Errorf("got %q", k)
	}
}

func TestParseKittyCSIuEventType(t *testing.T) {
	data := []byte("\x1b[97;1:3u")
	if !IsKeyRelease(data) {
		t.Error("expected key release")
	}
}

func TestIsKeyReleaseLegacy(t *testing.T) {
	if IsKeyRelease([]byte("a")) {
		t.Error("plain byte is not a release")
	}
}

func TestMatchesKey(t *testing.T) {
	if !MatchesKey([]byte{0x03}, "ctrl+c") {
		t.Error("ctrl+c should match")
	}
	if MatchesKey([]byte{0x03}, "ctrl+d") {
		t.Error("ctrl+c should not match ctrl+d")
	}
}

func TestCtrlChar(t *testing.T) {
	if CtrlChar('a') != 1 {
		t.Errorf("ctrl+a = %d", CtrlChar('a'))
	}
	if CtrlChar('Z') != 26 {
		t.Errorf("ctrl+Z = %d", CtrlChar('Z'))
	}
	if CtrlChar('0') != 0 {
		t.Errorf("ctrl+0 should be 0, got %d", CtrlChar('0'))
	}
}

func TestParseUnknown(t *testing.T) {
	_, ok := ParseKey([]byte{0xFF, 0xFE})
	if ok {
		t.Error("unknown sequence should return false")
	}
}
