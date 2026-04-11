package tui

import (
	"bytes"
	"testing"
)

func TestSplitSequencesSingleChar(t *testing.T) {
	got := splitSequences([]byte("a"))
	if len(got) != 1 || !bytes.Equal(got[0], []byte("a")) {
		t.Errorf("got %v", got)
	}
}

func TestSplitSequencesMultipleChars(t *testing.T) {
	got := splitSequences([]byte("abc"))
	if len(got) != 3 {
		t.Errorf("got %d", len(got))
	}
}

func TestSplitSequencesCSI(t *testing.T) {
	got := splitSequences([]byte("\x1b[A"))
	if len(got) != 1 || !bytes.Equal(got[0], []byte("\x1b[A")) {
		t.Errorf("got %v", got)
	}
}

func TestSplitSequencesBatched(t *testing.T) {
	// Three sequences: up arrow, 'a', ctrl+c
	got := splitSequences([]byte("\x1b[Aa\x03"))
	if len(got) != 3 {
		t.Errorf("got %d seqs: %v", len(got), got)
	}
	if !bytes.Equal(got[0], []byte("\x1b[A")) {
		t.Errorf("seq[0] = %v", got[0])
	}
	if !bytes.Equal(got[1], []byte("a")) {
		t.Errorf("seq[1] = %v", got[1])
	}
	if !bytes.Equal(got[2], []byte{0x03}) {
		t.Errorf("seq[2] = %v", got[2])
	}
}

func TestSplitSequencesSS3(t *testing.T) {
	got := splitSequences([]byte("\x1bOA"))
	if len(got) != 1 || !bytes.Equal(got[0], []byte("\x1bOA")) {
		t.Errorf("got %v", got)
	}
}

func TestSplitSequencesAltLetter(t *testing.T) {
	got := splitSequences([]byte("\x1bb"))
	if len(got) != 1 || !bytes.Equal(got[0], []byte("\x1bb")) {
		t.Errorf("got %v", got)
	}
}

func TestSplitSequencesLoneEscape(t *testing.T) {
	got := splitSequences([]byte{0x1B})
	if len(got) != 1 || len(got[0]) != 1 {
		t.Errorf("got %v", got)
	}
}

func TestSplitSequencesOSC(t *testing.T) {
	got := splitSequences([]byte("\x1b]0;title\x07"))
	if len(got) != 1 {
		t.Errorf("got %v", got)
	}
}

func TestSplitSequencesUTF8(t *testing.T) {
	got := splitSequences([]byte("日本"))
	if len(got) != 2 {
		t.Errorf("got %d", len(got))
	}
	if !bytes.Equal(got[0], []byte("日")) {
		t.Errorf("seq[0] = %v", got[0])
	}
}

func TestDecodeRuneASCII(t *testing.T) {
	r, n := decodeRune([]byte("a"))
	if r != 'a' || n != 1 {
		t.Errorf("got %c (%d)", r, n)
	}
}

func TestDecodeRuneUTF8(t *testing.T) {
	r, n := decodeRune([]byte("日"))
	if r != '日' || n != 3 {
		t.Errorf("got %c (%d)", r, n)
	}
}

func TestTerminalSatisfiesInterface(t *testing.T) {
	var _ TerminalIO = (*Terminal)(nil)
}

func TestTerminalStopIdempotent(t *testing.T) {
	// Can't start a real terminal in a test env, but Stop without Start
	// should be safe.
	tt := NewTerminal()
	tt.Stop()
	tt.Stop() // second call must not panic
}

func TestFindCSIEnd(t *testing.T) {
	data := []byte("\x1b[1;5Axyz")
	// Pass start = index after '['
	got := findCSIEnd(data, 2)
	if data[got] != 'A' {
		t.Errorf("expected terminator 'A', got %c", data[got])
	}
}

func TestFindSTEndBEL(t *testing.T) {
	data := []byte("title\x07")
	got := findSTEnd(data, 0)
	if got != len(data) {
		t.Errorf("got %d, want %d", got, len(data))
	}
}

func TestFindSTEndESCBackslash(t *testing.T) {
	data := []byte("title\x1b\\")
	got := findSTEnd(data, 0)
	if got != len(data) {
		t.Errorf("got %d, want %d", got, len(data))
	}
}
