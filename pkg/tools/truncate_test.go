package tools

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateHeadUnderLimits(t *testing.T) {
	content := "line1\nline2\nline3"
	r := TruncateHead(content, nil)
	if r.Truncated {
		t.Error("content under limits should not be truncated")
	}
	if r.Content != content {
		t.Errorf("content mismatch")
	}
}

func TestTruncateHeadByLines(t *testing.T) {
	lines := make([]string, 3000)
	for i := range lines {
		lines[i] = "x"
	}
	content := strings.Join(lines, "\n")

	r := TruncateHead(content, &TruncationOptions{MaxLines: 100, MaxBytes: 1 << 30})
	if !r.Truncated || r.TruncatedBy != "lines" {
		t.Errorf("got %+v", r)
	}
	if r.OutputLines != 100 {
		t.Errorf("output lines = %d", r.OutputLines)
	}
}

func TestTruncateHeadByBytes(t *testing.T) {
	content := strings.Repeat("abcdefghij\n", 100) // 1100 bytes
	r := TruncateHead(content, &TruncationOptions{MaxLines: 1000, MaxBytes: 50})
	if !r.Truncated || r.TruncatedBy != "bytes" {
		t.Errorf("got %+v", r)
	}
	if r.OutputBytes > 50 {
		t.Errorf("output exceeds limit: %d", r.OutputBytes)
	}
}

func TestTruncateHeadFirstLineExceedsLimit(t *testing.T) {
	content := strings.Repeat("x", 1000) + "\nnext"
	r := TruncateHead(content, &TruncationOptions{MaxBytes: 100})
	if !r.FirstLineExceedsLimit {
		t.Error("expected FirstLineExceedsLimit")
	}
	if r.Content != "" {
		t.Errorf("expected empty content, got %q", r.Content)
	}
}

func TestTruncateTailKeepsLastLines(t *testing.T) {
	lines := []string{"a", "b", "c", "d", "e"}
	r := TruncateTail(strings.Join(lines, "\n"), &TruncationOptions{MaxLines: 3, MaxBytes: 1000})
	if !r.Truncated || r.TruncatedBy != "lines" {
		t.Errorf("got %+v", r)
	}
	if r.Content != "c\nd\ne" {
		t.Errorf("content = %q", r.Content)
	}
}

func TestTruncateTailSingleHugeLine(t *testing.T) {
	content := strings.Repeat("x", 1000)
	r := TruncateTail(content, &TruncationOptions{MaxLines: 10, MaxBytes: 50})
	if !r.LastLinePartial {
		t.Error("expected LastLinePartial")
	}
	if r.OutputBytes > 50 {
		t.Errorf("output exceeds limit: %d", r.OutputBytes)
	}
}

func TestTruncateTailPreservesUTF8(t *testing.T) {
	// "héllo" repeated — multi-byte runes. Truncating should land on a rune boundary.
	content := strings.Repeat("héllo ", 100)
	r := TruncateTail(content, &TruncationOptions{MaxLines: 1, MaxBytes: 20})
	if !r.LastLinePartial {
		t.Error("expected LastLinePartial")
	}
	if !utf8.ValidString(r.Content) {
		t.Errorf("content is not valid UTF-8: %q", r.Content)
	}
}

func TestTruncateEmpty(t *testing.T) {
	r := TruncateHead("", nil)
	if r.Truncated {
		t.Errorf("empty content should not be truncated")
	}
	r2 := TruncateTail("", nil)
	if r2.Truncated {
		t.Errorf("empty content should not be truncated")
	}
}

func TestTruncateSingleLine(t *testing.T) {
	r := TruncateHead("only-line", nil)
	if r.Truncated {
		t.Error("single line should fit")
	}
}

func TestTruncateLine(t *testing.T) {
	long := strings.Repeat("a", 600)
	got := TruncateLine(long, 100)
	if !strings.Contains(got, "truncated") {
		t.Errorf("missing suffix: %q", got)
	}
	short := "short line"
	if TruncateLine(short, 100) != short {
		t.Error("short line should be unchanged")
	}
}

func TestFormatSize(t *testing.T) {
	cases := map[int]string{
		0:           "0B",
		500:         "500B",
		1024:        "1.0KB",
		1500:        "1.5KB",
		1024 * 1024: "1.0MB",
	}
	for bytes, want := range cases {
		if got := FormatSize(bytes); got != want {
			t.Errorf("FormatSize(%d) = %q, want %q", bytes, got, want)
		}
	}
}
