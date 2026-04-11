package tools

import (
	"strings"
	"testing"
)

// ----- line endings -----

func TestDetectLineEndingLF(t *testing.T) {
	if DetectLineEnding("line1\nline2\n") != "\n" {
		t.Error("LF not detected")
	}
}

func TestDetectLineEndingCRLF(t *testing.T) {
	if DetectLineEnding("line1\r\nline2\r\n") != "\r\n" {
		t.Error("CRLF not detected")
	}
}

func TestDetectLineEndingMixedPrefersFirst(t *testing.T) {
	// CRLF appears first.
	if DetectLineEnding("line1\r\nline2\nmore") != "\r\n" {
		t.Error("expected CRLF when CRLF comes first")
	}
	// LF appears first.
	if DetectLineEnding("line1\nline2\r\n") != "\n" {
		t.Error("expected LF when LF comes first")
	}
}

func TestNormalizeToLF(t *testing.T) {
	input := "a\r\nb\rc\nd"
	got := NormalizeToLF(input)
	if got != "a\nb\nc\nd" {
		t.Errorf("got %q", got)
	}
}

func TestRestoreLineEndings(t *testing.T) {
	if RestoreLineEndings("a\nb", "\r\n") != "a\r\nb" {
		t.Error("CRLF restore failed")
	}
	if RestoreLineEndings("a\nb", "\n") != "a\nb" {
		t.Error("LF should be a no-op")
	}
}

// ----- BOM -----

func TestStripBomWithBom(t *testing.T) {
	content := "\xEF\xBB\xBFhello"
	r := StripBom(content)
	if r.Bom == "" || r.Text != "hello" {
		t.Errorf("got %+v", r)
	}
}

func TestStripBomWithout(t *testing.T) {
	r := StripBom("hello")
	if r.Bom != "" || r.Text != "hello" {
		t.Errorf("got %+v", r)
	}
}

// ----- fuzzy match -----

func TestFuzzyFindExact(t *testing.T) {
	r := FuzzyFindText("hello world", "world")
	if !r.Found || r.Index != 6 {
		t.Errorf("got %+v", r)
	}
}

func TestFuzzyFindSmartQuotes(t *testing.T) {
	content := "hello \u201cworld\u201d"
	r := FuzzyFindText(content, `"world"`)
	if !r.Found {
		t.Errorf("should find smart-quote content via fuzzy match: %+v", r)
	}
}

func TestFuzzyFindUnicodeDashes(t *testing.T) {
	content := "hello\u2014world"
	r := FuzzyFindText(content, "hello-world")
	if !r.Found {
		t.Errorf("should find em-dash content: %+v", r)
	}
}

func TestFuzzyFindTrailingWhitespace(t *testing.T) {
	content := "line1   \nline2"
	r := FuzzyFindText(content, "line1\nline2")
	if !r.Found {
		t.Errorf("should strip trailing whitespace: %+v", r)
	}
}

func TestFuzzyFindNBSP(t *testing.T) {
	content := "hello\u00a0world"
	r := FuzzyFindText(content, "hello world")
	if !r.Found {
		t.Errorf("should treat NBSP as space: %+v", r)
	}
}

func TestFuzzyFindMiss(t *testing.T) {
	r := FuzzyFindText("hello", "missing")
	if r.Found {
		t.Error("should not find missing text")
	}
}

// ----- ApplyEdits -----

func TestApplyEditsSingle(t *testing.T) {
	result, err := ApplyEdits("hello world", []EditPair{{OldText: "world", NewText: "gcode"}}, "test.txt")
	if err != nil {
		t.Fatal(err)
	}
	if result.NewContent != "hello gcode" {
		t.Errorf("got %q", result.NewContent)
	}
}

func TestApplyEditsMultipleNonOverlapping(t *testing.T) {
	result, err := ApplyEdits("one two three", []EditPair{
		{OldText: "one", NewText: "ONE"},
		{OldText: "three", NewText: "THREE"},
	}, "test.txt")
	if err != nil {
		t.Fatal(err)
	}
	if result.NewContent != "ONE two THREE" {
		t.Errorf("got %q", result.NewContent)
	}
}

func TestApplyEditsOverlap(t *testing.T) {
	_, err := ApplyEdits("abcdef", []EditPair{
		{OldText: "abc", NewText: "X"},
		{OldText: "bcd", NewText: "Y"},
	}, "test.txt")
	if err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Errorf("expected overlap error, got %v", err)
	}
}

func TestApplyEditsNotFound(t *testing.T) {
	_, err := ApplyEdits("hello", []EditPair{{OldText: "world", NewText: "gcode"}}, "test.txt")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not-found error, got %v", err)
	}
}

func TestApplyEditsNotUnique(t *testing.T) {
	_, err := ApplyEdits("hello hello", []EditPair{{OldText: "hello", NewText: "hi"}}, "test.txt")
	if err == nil || !strings.Contains(err.Error(), "not unique") {
		t.Errorf("expected not-unique error, got %v", err)
	}
}

func TestApplyEditsEmptyOldText(t *testing.T) {
	_, err := ApplyEdits("hello", []EditPair{{OldText: "", NewText: "x"}}, "test.txt")
	if err == nil {
		t.Error("expected error for empty oldText")
	}
}

func TestApplyEditsNoChanges(t *testing.T) {
	_, err := ApplyEdits("hello", []EditPair{{OldText: "hello", NewText: "hello"}}, "test.txt")
	if err == nil || !strings.Contains(err.Error(), "no changes") {
		t.Errorf("expected no-changes error, got %v", err)
	}
}

func TestApplyEditsEmpty(t *testing.T) {
	_, err := ApplyEdits("hello", nil, "test.txt")
	if err == nil {
		t.Error("expected error for empty edits")
	}
}

// ----- GenerateDiff -----

func TestGenerateDiffSingleChange(t *testing.T) {
	old := "line1\nline2\nline3"
	new := "line1\nCHANGED\nline3"
	r := GenerateDiff(old, new, 2)
	if !strings.Contains(r.Diff, "-") || !strings.Contains(r.Diff, "+") {
		t.Errorf("diff missing +/- markers: %q", r.Diff)
	}
	if !strings.Contains(r.Diff, "CHANGED") {
		t.Errorf("diff missing new content: %q", r.Diff)
	}
	if r.FirstChangedLine == nil || *r.FirstChangedLine != 2 {
		t.Errorf("FirstChangedLine = %v", r.FirstChangedLine)
	}
}

func TestGenerateDiffNoChange(t *testing.T) {
	r := GenerateDiff("same", "same", 2)
	if r.Diff != "" {
		t.Errorf("no-change diff should be empty: %q", r.Diff)
	}
}

func TestGenerateDiffAdditions(t *testing.T) {
	r := GenerateDiff("a", "a\nb\nc", 0)
	if !strings.Contains(r.Diff, "+") {
		t.Errorf("missing additions: %q", r.Diff)
	}
}

func TestGenerateDiffDeletions(t *testing.T) {
	r := GenerateDiff("a\nb\nc", "a", 0)
	if !strings.Contains(r.Diff, "-") {
		t.Errorf("missing deletions: %q", r.Diff)
	}
}

func TestGenerateDiffContextLines(t *testing.T) {
	// Context lines should be labelled with ' '.
	old := "one\ntwo\nthree\nfour\nfive"
	new := "one\ntwo\nCHANGED\nfour\nfive"
	r := GenerateDiff(old, new, 1)
	if !strings.Contains(r.Diff, " ") {
		t.Errorf("missing context lines: %q", r.Diff)
	}
}
