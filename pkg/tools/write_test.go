package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteNewFile(t *testing.T) {
	dir := t.TempDir()
	r, err := executeWrite(dir, WriteParams{Path: "new.txt", Content: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "new.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Errorf("file = %q", string(data))
	}
	details := r.Details.(map[string]any)
	if details["bytes"] != 5 {
		t.Errorf("bytes = %v", details["bytes"])
	}
}

func TestWriteOverwrites(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "f.txt", "original")
	if _, err := executeWrite(dir, WriteParams{Path: "f.txt", Content: "replaced"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	if string(data) != "replaced" {
		t.Errorf("file = %q", string(data))
	}
}

func TestWriteCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	if _, err := executeWrite(dir, WriteParams{Path: "deep/nested/file.txt", Content: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "deep", "nested", "file.txt")); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestWriteLargeContent(t *testing.T) {
	dir := t.TempDir()
	content := strings.Repeat("x", 1024*1024) // 1MB
	if _, err := executeWrite(dir, WriteParams{Path: "big.txt", Content: content}); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(filepath.Join(dir, "big.txt"))
	if info.Size() != int64(len(content)) {
		t.Errorf("size = %d, want %d", info.Size(), len(content))
	}
}

func TestWriteEmptyPath(t *testing.T) {
	dir := t.TempDir()
	_, err := executeWrite(dir, WriteParams{Path: "", Content: "x"})
	if err == nil {
		t.Error("expected error for empty path")
	}
}

func TestWritePathTraversalBlocked(t *testing.T) {
	dir := t.TempDir()
	_, err := executeWrite(dir, WriteParams{Path: "../escape.txt", Content: "x"})
	if err == nil {
		t.Error("expected escape error")
	}
}
