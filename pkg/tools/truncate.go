package tools

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Default limits used by the read, bash, and fetch tools.
const (
	DefaultMaxLines   = 2000
	DefaultMaxBytes   = 50 * 1024
	GrepMaxLineLength = 500
)

// TruncationResult captures everything a caller needs to format a
// "content was truncated" message.
type TruncationResult struct {
	Content               string
	Truncated             bool
	TruncatedBy           string // "lines" | "bytes" | ""
	TotalLines            int
	TotalBytes            int
	OutputLines           int
	OutputBytes           int
	LastLinePartial       bool
	FirstLineExceedsLimit bool
	MaxLines              int
	MaxBytes              int
}

// TruncationOptions lets callers override the default limits. Zero values
// fall back to the package defaults.
type TruncationOptions struct {
	MaxLines int
	MaxBytes int
}

func resolveOpts(opts *TruncationOptions) (maxLines, maxBytes int) {
	maxLines, maxBytes = DefaultMaxLines, DefaultMaxBytes
	if opts == nil {
		return
	}
	if opts.MaxLines > 0 {
		maxLines = opts.MaxLines
	}
	if opts.MaxBytes > 0 {
		maxBytes = opts.MaxBytes
	}
	return
}

// TruncateHead keeps content from the start and stops at the first limit
// hit. Used by the read and fetch tools. The returned content never
// contains a partial line.
func TruncateHead(content string, opts *TruncationOptions) TruncationResult {
	maxLines, maxBytes := resolveOpts(opts)
	totalBytes := len(content)
	lines := splitLines(content)
	totalLines := len(lines)

	res := TruncationResult{
		TotalLines:  totalLines,
		TotalBytes:  totalBytes,
		MaxLines:    maxLines,
		MaxBytes:    maxBytes,
		Content:     content,
		OutputLines: totalLines,
		OutputBytes: totalBytes,
	}

	if totalLines <= maxLines && totalBytes <= maxBytes {
		return res
	}

	// First-line overflow — caller must fall back to a different strategy.
	if len(lines) > 0 && len(lines[0]) > maxBytes {
		return TruncationResult{
			Content:               "",
			Truncated:             true,
			TruncatedBy:           "bytes",
			TotalLines:            totalLines,
			TotalBytes:            totalBytes,
			OutputLines:           0,
			OutputBytes:           0,
			FirstLineExceedsLimit: true,
			MaxLines:              maxLines,
			MaxBytes:              maxBytes,
		}
	}

	collected := make([]string, 0, maxLines)
	acc := 0
	truncatedBy := ""

	for i, line := range lines {
		if i >= maxLines {
			truncatedBy = "lines"
			break
		}
		next := acc + len(line)
		if i < len(lines)-1 {
			next++ // newline separator
		}
		if next > maxBytes {
			truncatedBy = "bytes"
			break
		}
		collected = append(collected, line)
		acc = next
	}

	out := strings.Join(collected, "\n")
	return TruncationResult{
		Content:     out,
		Truncated:   true,
		TruncatedBy: truncatedBy,
		TotalLines:  totalLines,
		TotalBytes:  totalBytes,
		OutputLines: len(collected),
		OutputBytes: len(out),
		MaxLines:    maxLines,
		MaxBytes:    maxBytes,
	}
}

// TruncateTail keeps the tail of the content. Used by the bash tool so the
// model sees the most recent output when the command runs over the limit.
// When even a single tail line would exceed the byte limit, the last line
// is truncated at a valid UTF-8 boundary and marked partial.
func TruncateTail(content string, opts *TruncationOptions) TruncationResult {
	maxLines, maxBytes := resolveOpts(opts)
	totalBytes := len(content)
	lines := splitLines(content)
	totalLines := len(lines)

	res := TruncationResult{
		TotalLines:  totalLines,
		TotalBytes:  totalBytes,
		MaxLines:    maxLines,
		MaxBytes:    maxBytes,
		Content:     content,
		OutputLines: totalLines,
		OutputBytes: totalBytes,
	}

	if totalLines <= maxLines && totalBytes <= maxBytes {
		return res
	}

	var collected []string
	acc := 0
	truncatedBy := ""

	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if len(collected) >= maxLines {
			truncatedBy = "lines"
			break
		}
		add := len(line)
		if len(collected) > 0 {
			add++ // newline between the already-collected and this line
		}
		if acc+add > maxBytes {
			truncatedBy = "bytes"
			break
		}
		collected = append([]string{line}, collected...)
		acc += add
	}

	if len(collected) == 0 && len(lines) > 0 {
		// The tail line alone exceeds the byte limit; return the tail of it.
		last := lines[len(lines)-1]
		start := len(last) - maxBytes
		if start < 0 {
			start = 0
		}
		start = findUTF8Start(last, start)
		partial := last[start:]
		return TruncationResult{
			Content:         partial,
			Truncated:       true,
			TruncatedBy:     "bytes",
			TotalLines:      totalLines,
			TotalBytes:      totalBytes,
			OutputLines:     1,
			OutputBytes:     len(partial),
			LastLinePartial: true,
			MaxLines:        maxLines,
			MaxBytes:        maxBytes,
		}
	}

	out := strings.Join(collected, "\n")
	return TruncationResult{
		Content:     out,
		Truncated:   true,
		TruncatedBy: truncatedBy,
		TotalLines:  totalLines,
		TotalBytes:  totalBytes,
		OutputLines: len(collected),
		OutputBytes: len(out),
		MaxLines:    maxLines,
		MaxBytes:    maxBytes,
	}
}

// TruncateLine cuts a single line to maxChars and appends a suffix if the
// line was actually trimmed. Useful for grep-style output where individual
// matches can be enormous.
func TruncateLine(line string, maxChars int) string {
	if maxChars <= 0 || utf8.RuneCountInString(line) <= maxChars {
		return line
	}
	// Walk runes, keep the first maxChars.
	var b strings.Builder
	count := 0
	for _, r := range line {
		if count >= maxChars {
			break
		}
		b.WriteRune(r)
		count++
	}
	b.WriteString("... [truncated]")
	return b.String()
}

// FormatSize returns a human-readable size string.
func FormatSize(bytes int) string {
	const (
		KB = 1024
		MB = 1024 * 1024
	)
	switch {
	case bytes >= MB:
		return fmt.Sprintf("%.1fMB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1fKB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}

// splitLines splits on \n without keeping the separator. A trailing newline
// produces a final empty element, matching the behaviour of strings.Split.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// findUTF8Start returns an offset <= start that is at a valid UTF-8 rune
// boundary.
func findUTF8Start(s string, start int) int {
	if start >= len(s) {
		return len(s)
	}
	for start > 0 && !utf8.RuneStart(s[start]) {
		start--
	}
	return start
}
