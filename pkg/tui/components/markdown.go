package components

import (
	"strings"
	"sync"

	"github.com/gmoigneu/gcode/pkg/tui"
)

// Markdown is a minimal markdown renderer: headers, bold/italic,
// inline code, fenced code blocks, bullet lists, blockquotes, and
// horizontal rules. Links are rendered as "[text](url)".
//
// The output is a plain []string of terminal-ready lines with ANSI
// styling — no dependency on a parser.
type Markdown struct {
	mu   sync.RWMutex
	text string
}

// NewMarkdown constructs a Markdown component.
func NewMarkdown(text string) *Markdown { return &Markdown{text: text} }

// SetText replaces the markdown source.
func (m *Markdown) SetText(text string) {
	m.mu.Lock()
	m.text = text
	m.mu.Unlock()
}

// Render returns the styled lines wrapped to width.
func (m *Markdown) Render(width int) []string {
	m.mu.RLock()
	text := m.text
	m.mu.RUnlock()
	if text == "" {
		return nil
	}
	raw := strings.Split(text, "\n")
	var out []string

	inCode := false
	for _, line := range raw {
		trimmed := strings.TrimSpace(line)

		// Fenced code blocks.
		if strings.HasPrefix(trimmed, "```") {
			inCode = !inCode
			continue
		}
		if inCode {
			out = append(out, renderCodeLine(line, width))
			continue
		}

		// Horizontal rule.
		if trimmed == "---" || trimmed == "***" || trimmed == "___" {
			out = append(out, strings.Repeat("─", width))
			continue
		}

		// Headers.
		if level, rest, ok := parseHeader(trimmed); ok {
			out = append(out, renderHeader(level, rest, width))
			continue
		}

		// Blockquote.
		if strings.HasPrefix(trimmed, "> ") {
			body := renderInline(strings.TrimPrefix(trimmed, "> "))
			styled := "\x1b[2m│\x1b[0m " + body
			out = append(out, wrapForTerminal(styled, width)...)
			continue
		}

		// Bullet list.
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			body := renderInline(trimmed[2:])
			out = append(out, wrapForTerminal("• "+body, width)...)
			continue
		}
		if num, rest, ok := parseOrderedListItem(trimmed); ok {
			body := renderInline(rest)
			prefix := num + ". "
			out = append(out, wrapForTerminal(prefix+body, width)...)
			continue
		}

		// Empty line.
		if trimmed == "" {
			out = append(out, "")
			continue
		}

		// Paragraph.
		out = append(out, wrapForTerminal(renderInline(line), width)...)
	}
	return out
}

// Invalidate is a no-op.
func (m *Markdown) Invalidate() {}

// ----- helpers -----

// parseHeader returns (level, rest, true) for a markdown header line.
func parseHeader(line string) (int, string, bool) {
	level := 0
	for level < len(line) && line[level] == '#' {
		level++
	}
	if level == 0 || level > 6 {
		return 0, "", false
	}
	if level == len(line) {
		return level, "", true
	}
	if line[level] != ' ' {
		return 0, "", false
	}
	return level, strings.TrimSpace(line[level:]), true
}

// parseOrderedListItem matches "1. text" at the start of a line.
func parseOrderedListItem(line string) (string, string, bool) {
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	if i == 0 || i >= len(line) || line[i] != '.' {
		return "", "", false
	}
	if i+1 >= len(line) || line[i+1] != ' ' {
		return "", "", false
	}
	return line[:i], strings.TrimSpace(line[i+2:]), true
}

// renderHeader applies a bold + color style based on header level.
func renderHeader(level int, text string, width int) string {
	// Heavier color for bigger headers.
	color := "\x1b[1;36m" // bold cyan
	if level >= 3 {
		color = "\x1b[1;34m" // bold blue
	}
	if level >= 5 {
		color = "\x1b[1m" // plain bold
	}
	styled := color + text + tui.Reset
	if tui.VisibleWidth(styled) > width {
		return tui.SliceByColumn(styled, 0, width)
	}
	return styled
}

// renderCodeLine renders a line inside a fenced code block with a dim
// background-less style.
func renderCodeLine(line string, width int) string {
	line = "  " + line
	if tui.VisibleWidth(line) > width {
		line = tui.SliceByColumn(line, 0, width)
	}
	return "\x1b[2m" + line + tui.Reset
}

// renderInline converts inline markdown tokens to ANSI styled text.
// Handles: `code`, **bold**, *italic*, ~~strike~~, [label](url).
func renderInline(text string) string {
	var b strings.Builder
	i := 0
	for i < len(text) {
		c := text[i]
		switch c {
		case '`':
			end := strings.IndexByte(text[i+1:], '`')
			if end < 0 {
				b.WriteByte(c)
				i++
				continue
			}
			body := text[i+1 : i+1+end]
			b.WriteString("\x1b[7m" + body + tui.Reset)
			i = i + 2 + end
		case '*':
			// **bold** or *italic*
			if i+1 < len(text) && text[i+1] == '*' {
				end := strings.Index(text[i+2:], "**")
				if end >= 0 {
					body := text[i+2 : i+2+end]
					b.WriteString("\x1b[1m" + body + tui.Reset)
					i = i + 4 + end
					continue
				}
			}
			end := strings.IndexByte(text[i+1:], '*')
			if end >= 0 {
				body := text[i+1 : i+1+end]
				b.WriteString("\x1b[3m" + body + tui.Reset)
				i = i + 2 + end
				continue
			}
			b.WriteByte(c)
			i++
		case '[':
			// Link: [label](url)
			closeBracket := strings.IndexByte(text[i:], ']')
			if closeBracket < 0 || i+closeBracket+1 >= len(text) || text[i+closeBracket+1] != '(' {
				b.WriteByte(c)
				i++
				continue
			}
			closeParen := strings.IndexByte(text[i+closeBracket+2:], ')')
			if closeParen < 0 {
				b.WriteByte(c)
				i++
				continue
			}
			label := text[i+1 : i+closeBracket]
			url := text[i+closeBracket+2 : i+closeBracket+2+closeParen]
			b.WriteString("\x1b[4;34m" + label + tui.Reset + "\x1b[2m(" + url + ")" + tui.Reset)
			i = i + closeBracket + 2 + closeParen + 1
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// wrapForTerminal word-wraps a styled line to fit the given width.
func wrapForTerminal(line string, width int) []string {
	return wordWrap(line, width)
}
