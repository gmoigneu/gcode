package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gmoigneu/gcode/pkg/ai"
)

func TestEditSingleReplacement(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "f.txt", "hello world")

	r, err := executeEdit(dir, EditParams{
		Path:  "f.txt",
		Edits: []EditPair{{OldText: "world", NewText: "gcode"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(p)
	if string(got) != "hello gcode" {
		t.Errorf("file = %q", string(got))
	}
	if tc := r.Content[0].(*ai.TextContent); !strings.Contains(tc.Text, "Applied 1 edit") {
		t.Errorf("result = %q", tc.Text)
	}
}

func TestEditMultipleReplacements(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "f.txt", "one two three")

	_, err := executeEdit(dir, EditParams{
		Path: "f.txt",
		Edits: []EditPair{
			{OldText: "one", NewText: "ONE"},
			{OldText: "three", NewText: "THREE"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "ONE two THREE" {
		t.Errorf("file = %q", string(got))
	}
}

func TestEditPreservesBOM(t *testing.T) {
	dir := t.TempDir()
	content := "\xEF\xBB\xBFhello world"
	p := writeFile(t, dir, "f.txt", content)

	if _, err := executeEdit(dir, EditParams{
		Path:  "f.txt",
		Edits: []EditPair{{OldText: "world", NewText: "gcode"}},
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if !strings.HasPrefix(string(got), "\xEF\xBB\xBF") {
		t.Errorf("BOM lost: %q", string(got))
	}
	if !strings.Contains(string(got), "gcode") {
		t.Errorf("edit lost: %q", string(got))
	}
}

func TestEditPreservesCRLF(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "f.txt", "line1\r\nline2\r\nline3\r\n")

	if _, err := executeEdit(dir, EditParams{
		Path:  "f.txt",
		Edits: []EditPair{{OldText: "line2", NewText: "LINE2"}},
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if !strings.Contains(string(got), "\r\n") {
		t.Errorf("CRLF lost: %q", string(got))
	}
	if strings.Contains(string(got), "line2") {
		t.Errorf("edit not applied: %q", string(got))
	}
}

func TestEditFuzzyMatch(t *testing.T) {
	dir := t.TempDir()
	// File has smart quotes; edit uses ASCII quotes.
	p := writeFile(t, dir, "f.txt", "say \u201chello\u201d")

	if _, err := executeEdit(dir, EditParams{
		Path:  "f.txt",
		Edits: []EditPair{{OldText: `say "hello"`, NewText: `say "world"`}},
	}); err != nil {
		t.Fatalf("fuzzy edit failed: %v", err)
	}
	got, _ := os.ReadFile(p)
	if !strings.Contains(string(got), "world") {
		t.Errorf("file = %q", string(got))
	}
}

func TestEditNotFound(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "f.txt", "hello")

	_, err := executeEdit(dir, EditParams{
		Path:  "f.txt",
		Edits: []EditPair{{OldText: "missing", NewText: "x"}},
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not-found error, got %v", err)
	}
}

func TestEditNotUnique(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "f.txt", "hello hello")

	_, err := executeEdit(dir, EditParams{
		Path:  "f.txt",
		Edits: []EditPair{{OldText: "hello", NewText: "hi"}},
	})
	if err == nil || !strings.Contains(err.Error(), "not unique") {
		t.Errorf("expected not-unique error, got %v", err)
	}
}

func TestEditOverlap(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "f.txt", "abcdef")

	_, err := executeEdit(dir, EditParams{
		Path: "f.txt",
		Edits: []EditPair{
			{OldText: "abc", NewText: "X"},
			{OldText: "bcd", NewText: "Y"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Errorf("expected overlap error, got %v", err)
	}
}

func TestEditEmpty(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "f.txt", "hello")
	_, err := executeEdit(dir, EditParams{Path: "f.txt"})
	if err == nil {
		t.Error("expected error for empty edits")
	}
}

func TestEditMissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := executeEdit(dir, EditParams{
		Path:  "nope.txt",
		Edits: []EditPair{{OldText: "x", NewText: "y"}},
	})
	if err == nil {
		t.Error("expected error")
	}
}

func TestEditPrepareArgumentsLegacy(t *testing.T) {
	args := map[string]any{
		"path":    "f.txt",
		"oldText": "foo",
		"newText": "bar",
	}
	prepared := prepareEditArguments(args)
	edits, ok := prepared["edits"].([]any)
	if !ok || len(edits) != 1 {
		t.Fatalf("edits = %v", prepared["edits"])
	}
	e := edits[0].(map[string]any)
	if e["oldText"] != "foo" || e["newText"] != "bar" {
		t.Errorf("edit pair = %+v", e)
	}
	if _, stillHas := prepared["oldText"]; stillHas {
		t.Error("oldText should be removed")
	}
}

func TestEditPrepareArgumentsPassthrough(t *testing.T) {
	args := map[string]any{
		"path":  "f.txt",
		"edits": []any{map[string]any{"oldText": "x", "newText": "y"}},
	}
	prepared := prepareEditArguments(args)
	if edits, ok := prepared["edits"].([]any); !ok || len(edits) != 1 {
		t.Errorf("edits should pass through: %+v", prepared)
	}
}

func TestEditProducesDiff(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "f.txt", "line1\nline2\nline3")

	r, err := executeEdit(dir, EditParams{
		Path:  "f.txt",
		Edits: []EditPair{{OldText: "line2", NewText: "LINE2"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	tc := r.Content[0].(*ai.TextContent)
	if !strings.Contains(tc.Text, "LINE2") {
		t.Errorf("diff missing new content: %q", tc.Text)
	}
	details := r.Details.(map[string]any)
	if _, ok := details["firstChangedLine"]; !ok {
		t.Error("details missing firstChangedLine")
	}
}

func TestEditPathTraversalBlocked(t *testing.T) {
	dir := t.TempDir()
	_, err := executeEdit(dir, EditParams{
		Path:  "../escape.txt",
		Edits: []EditPair{{OldText: "x", NewText: "y"}},
	})
	if err == nil {
		t.Error("expected escape error")
	}
}

// Guard: ensure test helpers remain used.
var _ = filepath.Join
