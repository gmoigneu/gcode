package tui

import (
	"strings"
	"testing"
)

func TestVisibleWidthPlain(t *testing.T) {
	if got := VisibleWidth("hello"); got != 5 {
		t.Errorf("got %d", got)
	}
	if got := VisibleWidth(""); got != 0 {
		t.Errorf("empty = %d", got)
	}
}

func TestVisibleWidthSGR(t *testing.T) {
	// Red "hello" reset.
	s := "\x1b[31mhello\x1b[0m"
	if got := VisibleWidth(s); got != 5 {
		t.Errorf("got %d, want 5", got)
	}
}

func TestVisibleWidthOSCHyperlink(t *testing.T) {
	// Hyperlink: ESC ] 8 ; ; url BEL label ESC ] 8 ; ; BEL
	s := "\x1b]8;;https://example.com\x07link\x1b]8;;\x07"
	if got := VisibleWidth(s); got != 4 {
		t.Errorf("got %d, want 4", got)
	}
}

func TestVisibleWidthAPC(t *testing.T) {
	// APC sequence containing the CursorMarker shape should be stripped.
	s := "before\x1b_gc:c\x07after"
	if got := VisibleWidth(s); got != 11 {
		t.Errorf("got %d, want 11", got)
	}
}

func TestVisibleWidthWideChars(t *testing.T) {
	// Each CJK char has width 2.
	s := "日本語"
	if got := VisibleWidth(s); got != 6 {
		t.Errorf("got %d, want 6", got)
	}
}

func TestVisibleWidthMixed(t *testing.T) {
	s := "\x1b[1mHi 日本\x1b[0m!"
	// H(1) i(1) space(1) 日(2) 本(2) !(1) = 8
	if got := VisibleWidth(s); got != 8 {
		t.Errorf("got %d, want 8", got)
	}
}

func TestSliceByColumnPlain(t *testing.T) {
	got := SliceByColumn("0123456789", 2, 6)
	if got != "2345" {
		t.Errorf("got %q", got)
	}
}

func TestSliceByColumnWithANSI(t *testing.T) {
	s := "\x1b[31m0123456789\x1b[0m"
	got := SliceByColumn(s, 2, 6)
	// Should preserve the leading SGR (so color applies).
	if !strings.Contains(got, "2345") {
		t.Errorf("got %q", got)
	}
	if !strings.Contains(got, "\x1b[31m") {
		t.Errorf("leading SGR lost: %q", got)
	}
}

func TestSliceByColumnBeyondEnd(t *testing.T) {
	if got := SliceByColumn("abc", 0, 10); got != "abc" {
		t.Errorf("got %q", got)
	}
}

func TestSliceByColumnEmpty(t *testing.T) {
	if got := SliceByColumn("abc", 0, 0); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestPadToWidthShorter(t *testing.T) {
	if got := PadToWidth("hi", 5); got != "hi   " {
		t.Errorf("got %q", got)
	}
}

func TestPadToWidthAlready(t *testing.T) {
	if got := PadToWidth("hello", 5); got != "hello" {
		t.Errorf("got %q", got)
	}
}

func TestPadToWidthANSIIgnored(t *testing.T) {
	// Visible width should determine padding, not raw length.
	s := "\x1b[31mhi\x1b[0m"
	got := PadToWidth(s, 5)
	if VisibleWidth(got) != 5 {
		t.Errorf("padded width = %d", VisibleWidth(got))
	}
}
