package components

import (
	"strings"
	"testing"

	"github.com/gmoigneu/gcode/pkg/tui"
)

func TestMarkdownEmpty(t *testing.T) {
	if got := NewMarkdown("").Render(80); got != nil {
		t.Errorf("got %v", got)
	}
}

func TestMarkdownParagraph(t *testing.T) {
	m := NewMarkdown("hello world")
	got := m.Render(80)
	if len(got) != 1 || !strings.Contains(got[0], "hello world") {
		t.Errorf("got %v", got)
	}
}

func TestMarkdownH1(t *testing.T) {
	m := NewMarkdown("# Title")
	got := m.Render(80)[0]
	if !strings.Contains(got, "Title") {
		t.Errorf("got %q", got)
	}
	if !strings.Contains(got, "\x1b[") {
		t.Errorf("no ANSI styling: %q", got)
	}
}

func TestMarkdownH3(t *testing.T) {
	m := NewMarkdown("### Smaller")
	got := m.Render(80)[0]
	if !strings.Contains(got, "Smaller") {
		t.Errorf("got %q", got)
	}
}

func TestMarkdownBold(t *testing.T) {
	m := NewMarkdown("This is **bold** text")
	got := m.Render(80)[0]
	if !strings.Contains(got, "bold") {
		t.Errorf("got %q", got)
	}
	if !strings.Contains(got, "\x1b[1m") {
		t.Errorf("bold SGR missing: %q", got)
	}
}

func TestMarkdownItalic(t *testing.T) {
	m := NewMarkdown("This is *italic* text")
	got := m.Render(80)[0]
	if !strings.Contains(got, "\x1b[3m") {
		t.Errorf("italic SGR missing: %q", got)
	}
}

func TestMarkdownInlineCode(t *testing.T) {
	m := NewMarkdown("Use `code` here")
	got := m.Render(80)[0]
	if !strings.Contains(got, "\x1b[7m") {
		t.Errorf("code SGR missing: %q", got)
	}
}

func TestMarkdownCodeBlock(t *testing.T) {
	src := "```\nfunc main() {}\n```"
	m := NewMarkdown(src)
	got := m.Render(80)
	if len(got) != 1 {
		t.Fatalf("got %d lines", len(got))
	}
	if !strings.Contains(got[0], "func main") {
		t.Errorf("got %q", got[0])
	}
	if !strings.Contains(got[0], "\x1b[2m") {
		t.Errorf("code dim SGR missing: %q", got[0])
	}
}

func TestMarkdownBulletList(t *testing.T) {
	m := NewMarkdown("- apple\n- banana")
	got := m.Render(80)
	if len(got) != 2 {
		t.Fatalf("got %d lines: %v", len(got), got)
	}
	for _, line := range got {
		if !strings.Contains(line, "•") {
			t.Errorf("bullet missing: %q", line)
		}
	}
}

func TestMarkdownOrderedList(t *testing.T) {
	m := NewMarkdown("1. first\n2. second")
	got := m.Render(80)
	if !strings.Contains(got[0], "1.") || !strings.Contains(got[1], "2.") {
		t.Errorf("got %v", got)
	}
}

func TestMarkdownBlockquote(t *testing.T) {
	m := NewMarkdown("> quoted text")
	got := m.Render(80)[0]
	if !strings.Contains(got, "quoted text") {
		t.Errorf("body missing: %q", got)
	}
	if !strings.Contains(got, "│") {
		t.Errorf("blockquote bar missing: %q", got)
	}
}

func TestMarkdownLink(t *testing.T) {
	m := NewMarkdown("See [homepage](https://example.com)")
	got := m.Render(80)[0]
	if !strings.Contains(got, "homepage") || !strings.Contains(got, "https://example.com") {
		t.Errorf("got %q", got)
	}
}

func TestMarkdownHorizontalRule(t *testing.T) {
	m := NewMarkdown("---")
	got := m.Render(20)[0]
	if tui.VisibleWidth(got) != 20 {
		t.Errorf("hr width = %d", tui.VisibleWidth(got))
	}
	if !strings.Contains(got, "─") {
		t.Errorf("hr missing: %q", got)
	}
}

func TestMarkdownWordWrap(t *testing.T) {
	m := NewMarkdown("one two three four five six seven eight nine ten")
	got := m.Render(12)
	if len(got) < 2 {
		t.Errorf("expected wrap, got %v", got)
	}
}

func TestMarkdownMixed(t *testing.T) {
	src := "# Title\n\nSome **bold** text.\n\n- item\n\n```\ncode\n```"
	got := NewMarkdown(src).Render(80)
	if len(got) < 5 {
		t.Fatalf("got %v", got)
	}
}
