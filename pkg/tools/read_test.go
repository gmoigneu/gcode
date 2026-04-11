package tools

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gmoigneu/gcode/pkg/ai"
)

func intPtr(n int) *int { return &n }

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadTextFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "hello.txt", "hello\nworld\n")

	r, err := executeRead(dir, ReadParams{Path: "hello.txt"})
	if err != nil {
		t.Fatal(err)
	}
	tc := r.Content[0].(*ai.TextContent)
	if !strings.Contains(tc.Text, "hello") || !strings.Contains(tc.Text, "world") {
		t.Errorf("body = %q", tc.Text)
	}
}

func TestReadOffsetAndLimit(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "nums.txt", "one\ntwo\nthree\nfour\nfive")

	r, err := executeRead(dir, ReadParams{Path: "nums.txt", Offset: intPtr(2), Limit: intPtr(2)})
	if err != nil {
		t.Fatal(err)
	}
	tc := r.Content[0].(*ai.TextContent)
	if !strings.Contains(tc.Text, "two") || !strings.Contains(tc.Text, "three") {
		t.Errorf("body = %q", tc.Text)
	}
	if strings.Contains(tc.Text, "one") || strings.Contains(tc.Text, "four") {
		t.Errorf("offset/limit not applied: %q", tc.Text)
	}
	if !strings.Contains(tc.Text, "more lines in file") {
		t.Errorf("continuation hint missing: %q", tc.Text)
	}
}

func TestReadOffsetBeyondEOF(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "short.txt", "only\n")

	_, err := executeRead(dir, ReadParams{Path: "short.txt", Offset: intPtr(100)})
	if err == nil || !strings.Contains(err.Error(), "exceeds file length") {
		t.Errorf("expected offset error, got %v", err)
	}
}

func TestReadFileNotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := executeRead(dir, ReadParams{Path: "missing.txt"})
	if err == nil {
		t.Error("expected error")
	}
}

func TestReadLargeFileTruncated(t *testing.T) {
	dir := t.TempDir()
	// 3000 lines of "line N"
	var b strings.Builder
	for i := 1; i <= 3000; i++ {
		b.WriteString("line ")
		b.WriteString(strings.Repeat("a", 10))
		b.WriteString("\n")
	}
	writeFile(t, dir, "big.txt", b.String())

	r, err := executeRead(dir, ReadParams{Path: "big.txt"})
	if err != nil {
		t.Fatal(err)
	}
	tc := r.Content[0].(*ai.TextContent)
	if !strings.Contains(tc.Text, "Showing lines") {
		t.Errorf("truncation suffix missing: %q", tc.Text[len(tc.Text)-200:])
	}
}

func TestReadImageFile(t *testing.T) {
	dir := t.TempDir()
	// Minimal fake PNG bytes (not a real image, but the tool only cares about extension).
	raw := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x01}
	if err := os.WriteFile(filepath.Join(dir, "pic.png"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := executeRead(dir, ReadParams{Path: "pic.png"})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(r.Content))
	}
	if _, ok := r.Content[0].(*ai.TextContent); !ok {
		t.Errorf("content[0] = %T", r.Content[0])
	}
	img, ok := r.Content[1].(*ai.ImageContent)
	if !ok {
		t.Fatalf("content[1] = %T", r.Content[1])
	}
	if img.MimeType != "image/png" {
		t.Errorf("mime = %q", img.MimeType)
	}
	decoded, err := base64.StdEncoding.DecodeString(img.Data)
	if err != nil {
		t.Fatalf("invalid base64: %v", err)
	}
	if len(decoded) != len(raw) {
		t.Errorf("decoded length = %d", len(decoded))
	}
}

func TestReadDirectoryErrors(t *testing.T) {
	dir := t.TempDir()
	_, err := executeRead(filepath.Dir(dir), ReadParams{Path: filepath.Base(dir)})
	if err == nil {
		t.Error("expected error when reading a directory")
	}
}

func TestReadPathTraversalBlocked(t *testing.T) {
	dir := t.TempDir()
	_, err := executeRead(dir, ReadParams{Path: "../../etc/passwd"})
	if err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Errorf("expected escape error, got %v", err)
	}
}

func TestReadAbsolutePathAllowed(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "abs.txt", "content")
	r, err := executeRead(dir, ReadParams{Path: p})
	if err != nil {
		t.Fatal(err)
	}
	if tc := r.Content[0].(*ai.TextContent); !strings.Contains(tc.Text, "content") {
		t.Errorf("body = %q", tc.Text)
	}
}
