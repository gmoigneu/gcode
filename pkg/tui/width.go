package tui

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

// VisibleWidth returns the number of terminal columns a string will
// occupy when rendered, ignoring ANSI escape sequences and honouring
// East-Asian wide characters via go-runewidth.
func VisibleWidth(s string) int {
	if s == "" {
		return 0
	}
	width := 0
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == 0x1B {
			skip := escapeSkip(runes, i)
			if skip > 0 {
				i += skip - 1 // -1 because the loop increments i
				continue
			}
		}
		width += runewidth.RuneWidth(r)
	}
	return width
}

// escapeSkip counts how many runes (starting at i in runes) belong to an
// escape sequence. Returns 0 if the position is not the start of a
// recognised sequence.
func escapeSkip(runes []rune, i int) int {
	if i >= len(runes) || runes[i] != 0x1B {
		return 0
	}
	if i+1 >= len(runes) {
		// Bare ESC at end of string — treat as zero-width to avoid double
		// counting. It's not a valid sequence but we don't want to crash.
		return 1
	}
	switch runes[i+1] {
	case '[':
		// CSI: ends at any byte in 0x40..0x7E (@-~).
		for j := i + 2; j < len(runes); j++ {
			r := runes[j]
			if r >= '@' && r <= '~' {
				return j - i + 1
			}
		}
		return len(runes) - i
	case ']':
		// OSC: ends at BEL (0x07) or ST (ESC \\).
		for j := i + 2; j < len(runes); j++ {
			if runes[j] == 0x07 {
				return j - i + 1
			}
			if runes[j] == 0x1B && j+1 < len(runes) && runes[j+1] == '\\' {
				return j - i + 2
			}
		}
		return len(runes) - i
	case '_', 'P', '^':
		// APC / DCS / PM: terminated by ST (ESC \\).
		for j := i + 2; j < len(runes); j++ {
			if runes[j] == 0x1B && j+1 < len(runes) && runes[j+1] == '\\' {
				return j - i + 2
			}
			if runes[j] == 0x07 {
				return j - i + 1
			}
		}
		return len(runes) - i
	case 'O':
		// SS3 single-character function sequence.
		if i+2 < len(runes) {
			return 3
		}
		return 2
	}
	// Two-byte ESC+char (e.g. reset).
	return 2
}

// SliceByColumn returns the portion of s that corresponds to visible
// columns [startCol, endCol). ANSI escape sequences that appear before
// the selected slice are included verbatim at the front so they don't
// "leak" their styling onto characters that were sliced away. The
// function does NOT split a wide character; a wide rune at the boundary
// is either fully included (if it fits) or skipped entirely.
func SliceByColumn(s string, startCol, endCol int) string {
	if endCol <= startCol {
		return ""
	}
	var prefix strings.Builder
	var out strings.Builder
	col := 0
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == 0x1B {
			n := escapeSkip(runes, i)
			if n > 0 {
				seg := string(runes[i : i+n])
				if col < startCol {
					prefix.WriteString(seg)
				} else {
					out.WriteString(seg)
				}
				i += n - 1
				continue
			}
		}
		w := runewidth.RuneWidth(r)
		if col+w <= startCol {
			col += w
			continue
		}
		if col >= endCol {
			break
		}
		// The rune may straddle the start boundary (wide char at startCol-1).
		// Skip it entirely rather than corrupt UTF-8.
		if col < startCol {
			col += w
			continue
		}
		// Or straddle the end boundary; same treatment.
		if col+w > endCol {
			col = endCol
			break
		}
		out.WriteRune(r)
		col += w
	}
	return prefix.String() + out.String()
}

// PadToWidth appends spaces to s so its visible width equals target. If
// s already reaches or exceeds target the input is returned unchanged.
func PadToWidth(s string, target int) string {
	w := VisibleWidth(s)
	if w >= target {
		return s
	}
	return s + strings.Repeat(" ", target-w)
}
