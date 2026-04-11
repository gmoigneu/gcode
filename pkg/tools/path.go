package tools

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ResolveToCwd returns an absolute path for p. Relative paths are joined
// with cwd. Absolute paths are accepted as-is (the caller is assumed to be
// a trusted agent). Parent-traversal is still blocked for relative inputs
// to prevent the LLM from silently escaping the working directory.
func ResolveToCwd(p, cwd string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("path: empty path")
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p), nil
	}
	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("path: resolve cwd: %w", err)
	}
	joined := filepath.Clean(filepath.Join(absCwd, p))
	// Ensure the joined path is still inside cwd.
	rel, err := filepath.Rel(absCwd, joined)
	if err != nil {
		return "", fmt.Errorf("path: rel: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path: %q escapes working directory", p)
	}
	return joined, nil
}
