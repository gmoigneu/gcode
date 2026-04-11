package components

import (
	"strings"
	"testing"
	"time"

	"github.com/gmoigneu/gcode/pkg/agent"
	"github.com/gmoigneu/gcode/pkg/tui"
)

// ----- Text -----

func TestTextRender(t *testing.T) {
	c := NewText("hello")
	got := c.Render(80)
	if len(got) != 1 || got[0] != "hello" {
		t.Errorf("got %v", got)
	}
}

func TestTextMultiline(t *testing.T) {
	c := NewText("a\nb\nc")
	got := c.Render(80)
	if len(got) != 3 {
		t.Errorf("got %v", got)
	}
}

func TestTextEmpty(t *testing.T) {
	if got := NewText("").Render(80); got != nil {
		t.Errorf("got %v", got)
	}
}

func TestTextSetStyle(t *testing.T) {
	c := NewText("hi")
	c.SetStyle(func(s string) string { return "[" + s + "]" })
	got := c.Render(80)
	if got[0] != "[hi]" {
		t.Errorf("got %q", got[0])
	}
}

func TestTextClipsOverwide(t *testing.T) {
	c := NewText(strings.Repeat("x", 100))
	got := c.Render(10)
	if tui.VisibleWidth(got[0]) > 10 {
		t.Errorf("not clipped: %q", got[0])
	}
}

// ----- Container -----

func TestContainerConcatenates(t *testing.T) {
	c := NewContainer()
	c.AddChild(NewText("line1"))
	c.AddChild(NewText("line2"))
	got := c.Render(80)
	if len(got) != 2 || got[0] != "line1" || got[1] != "line2" {
		t.Errorf("got %v", got)
	}
}

func TestContainerRemoveChild(t *testing.T) {
	c := NewContainer()
	a := NewText("a")
	b := NewText("b")
	c.AddChild(a)
	c.AddChild(b)
	c.RemoveChild(a)
	got := c.Render(80)
	if len(got) != 1 || got[0] != "b" {
		t.Errorf("got %v", got)
	}
}

func TestContainerClear(t *testing.T) {
	c := NewContainer()
	c.AddChild(NewText("a"))
	c.Clear()
	if got := c.Render(80); got != nil {
		t.Errorf("got %v", got)
	}
}

func TestContainerInvalidateCascades(t *testing.T) {
	c := NewContainer()
	c.AddChild(NewText("x"))
	c.Invalidate() // must not panic
}

// ----- Box -----

func TestBoxRender(t *testing.T) {
	b := NewBox(NewText("hi"))
	got := b.Render(10)
	if len(got) != 3 {
		t.Fatalf("got %d lines: %v", len(got), got)
	}
	if !strings.HasPrefix(got[0], "┌") || !strings.HasSuffix(got[0], "┐") {
		t.Errorf("top border = %q", got[0])
	}
	if !strings.Contains(got[1], "hi") {
		t.Errorf("content missing: %q", got[1])
	}
	if !strings.HasPrefix(got[2], "└") || !strings.HasSuffix(got[2], "┘") {
		t.Errorf("bottom border = %q", got[2])
	}
}

func TestBoxTitle(t *testing.T) {
	b := NewBox(NewText("x")).WithTitle("Title")
	got := b.Render(20)
	if !strings.Contains(got[0], "Title") {
		t.Errorf("title missing: %q", got[0])
	}
}

func TestBoxStyles(t *testing.T) {
	b := NewBox(NewText("x")).WithStyle(BoxDouble)
	got := b.Render(10)
	if !strings.HasPrefix(got[0], "╔") {
		t.Errorf("double style not applied: %q", got[0])
	}
}

func TestBoxTooNarrow(t *testing.T) {
	b := NewBox(NewText("x"))
	if got := b.Render(1); got != nil {
		t.Errorf("expected nil for width 1, got %v", got)
	}
}

// ----- Spacer -----

func TestSpacer(t *testing.T) {
	s := NewSpacer(3)
	got := s.Render(10)
	if len(got) != 3 {
		t.Errorf("got %d", len(got))
	}
}

func TestSpacerZero(t *testing.T) {
	if got := NewSpacer(0).Render(10); got != nil {
		t.Error("zero spacer should render nothing")
	}
}

// ----- Loader -----

func TestLoaderIdleRendersNothing(t *testing.T) {
	l := NewLoader()
	if got := l.Render(80); got != nil {
		t.Errorf("idle should render nothing, got %v", got)
	}
}

func TestLoaderThinkingLabel(t *testing.T) {
	l := NewLoader()
	l.SetLiveness(agent.LivenessEvent{Status: agent.StatusThinking, Elapsed: 5 * time.Second})
	got := l.Render(80)
	if len(got) != 1 {
		t.Fatalf("got %d lines", len(got))
	}
	if !strings.Contains(stripANSI(got[0]), "Thinking") {
		t.Errorf("label missing: %q", got[0])
	}
	if !strings.Contains(got[0], "5s") {
		t.Errorf("elapsed missing: %q", got[0])
	}
}

func TestLoaderExecutingShowsTool(t *testing.T) {
	l := NewLoader()
	l.SetLiveness(agent.LivenessEvent{Status: agent.StatusExecuting, ToolName: "bash", Elapsed: 2 * time.Second})
	got := l.Render(80)[0]
	if !strings.Contains(stripANSI(got), "bash") {
		t.Errorf("tool name missing: %q", got)
	}
}

func TestLoaderStalled(t *testing.T) {
	l := NewLoader()
	l.SetLiveness(agent.LivenessEvent{Status: agent.StatusStalled, ToolName: "bash", Elapsed: 65 * time.Second})
	got := l.Render(80)[0]
	if !strings.Contains(stripANSI(got), "still running") {
		t.Errorf("stall label missing: %q", got)
	}
}

func TestLoaderTickAdvancesFrame(t *testing.T) {
	l := NewLoader()
	l.SetLiveness(agent.LivenessEvent{Status: agent.StatusThinking})
	frame1 := l.Render(80)[0]
	l.Tick()
	frame2 := l.Render(80)[0]
	if frame1 == frame2 {
		t.Error("frame should advance after Tick")
	}
}

// ----- StatusBar -----

func TestStatusBarModel(t *testing.T) {
	s := NewStatusBar()
	s.SetModel("claude-opus", "high")
	got := s.Render(80)[0]
	if !strings.Contains(stripANSI(got), "claude-opus") || !strings.Contains(stripANSI(got), "high") {
		t.Errorf("model missing: %q", got)
	}
}

func TestStatusBarUsage(t *testing.T) {
	s := NewStatusBar()
	s.SetUsage(1234, 0.05)
	got := s.Render(80)[0]
	if !strings.Contains(stripANSI(got), "1234 tok") {
		t.Errorf("tokens missing: %q", got)
	}
	if !strings.Contains(stripANSI(got), "$0.0500") {
		t.Errorf("cost missing: %q", got)
	}
}

func TestStatusBarTooNarrow(t *testing.T) {
	s := NewStatusBar()
	s.SetModel("very-long-model-name", "xhigh")
	s.SetUsage(9999999, 12.3456)
	got := s.Render(20)
	if tui.VisibleWidth(got[0]) > 20 {
		t.Errorf("not clipped: %q (width %d)", got[0], tui.VisibleWidth(got[0]))
	}
}

// ----- SelectList -----

func TestSelectListRender(t *testing.T) {
	items := []SelectItem{
		{Label: "first"},
		{Label: "second"},
		{Label: "third"},
	}
	s := NewSelectList(items, nil)
	got := s.Render(30)
	if len(got) != 3 {
		t.Errorf("len = %d", len(got))
	}
	if !strings.Contains(got[0], "first") {
		t.Errorf("got %v", got)
	}
}

func TestSelectListFilter(t *testing.T) {
	items := []SelectItem{
		{Label: "apple"},
		{Label: "banana"},
		{Label: "cherry"},
	}
	s := NewSelectList(items, nil)
	s.SetFilter("an")
	got := s.Render(30)
	// Only "banana" matches.
	if len(got) != 1 || !strings.Contains(got[0], "banana") {
		t.Errorf("got %v", got)
	}
}

func TestSelectListNavigation(t *testing.T) {
	items := []SelectItem{{Label: "a"}, {Label: "b"}, {Label: "c"}}
	s := NewSelectList(items, nil)
	s.SetFocused(true)

	// Initially first item selected.
	got, _ := s.Selected()
	if got.Label != "a" {
		t.Errorf("initial = %s", got.Label)
	}
	s.HandleInput([]byte("\x1b[B")) // down
	got, _ = s.Selected()
	if got.Label != "b" {
		t.Errorf("after down = %s", got.Label)
	}
	s.HandleInput([]byte("\x1b[A")) // up
	got, _ = s.Selected()
	if got.Label != "a" {
		t.Errorf("after up = %s", got.Label)
	}
}

func TestSelectListConfirmCallback(t *testing.T) {
	items := []SelectItem{{Label: "only", Value: 42}}
	s := NewSelectList(items, nil)
	s.SetFocused(true)
	var confirmed any
	s.OnConfirm = func(item SelectItem) { confirmed = item.Value }
	s.HandleInput([]byte{'\r'})
	if confirmed != 42 {
		t.Errorf("confirm = %v", confirmed)
	}
}

func TestSelectListCancelCallback(t *testing.T) {
	s := NewSelectList([]SelectItem{{Label: "x"}}, nil)
	s.SetFocused(true)
	cancelled := false
	s.OnCancel = func() { cancelled = true }
	s.HandleInput([]byte{0x1B}) // escape
	if !cancelled {
		t.Error("cancel not fired")
	}
}

func TestSelectListNoMatch(t *testing.T) {
	s := NewSelectList([]SelectItem{{Label: "x"}}, nil)
	s.SetFilter("nothing")
	got := s.Render(30)
	if !strings.Contains(got[0], "no matches") {
		t.Errorf("got %v", got)
	}
}

// ----- helper -----

func stripANSI(s string) string {
	// Re-use a tiny inline stripper for tests.
	var b strings.Builder
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] != 0x1B {
			b.WriteRune(runes[i])
			continue
		}
		// Skip until end of SGR-like sequence.
		j := i + 1
		for j < len(runes) && runes[j] != 'm' && runes[j] != '~' && runes[j] != 'H' {
			j++
		}
		if j < len(runes) {
			j++
		}
		i = j - 1
	}
	return b.String()
}
