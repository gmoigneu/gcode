package tools

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// ---- line endings ----

// DetectLineEnding returns "\r\n" if CRLF is observed before any bare LF.
// Otherwise it returns "\n". Used by the edit tool to round-trip files
// without mangling line endings.
func DetectLineEnding(content string) string {
	for i := 0; i < len(content); i++ {
		switch content[i] {
		case '\r':
			if i+1 < len(content) && content[i+1] == '\n' {
				return "\r\n"
			}
		case '\n':
			return "\n"
		}
	}
	return "\n"
}

// NormalizeToLF converts \r\n and bare \r to \n.
func NormalizeToLF(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return text
}

// RestoreLineEndings converts \n to the target ending. If the target is
// already "\n" the text is returned unchanged.
func RestoreLineEndings(text, ending string) string {
	if ending == "\n" || ending == "" {
		return text
	}
	return strings.ReplaceAll(text, "\n", ending)
}

// ---- BOM ----

// BomResult is the return value of StripBom.
type BomResult struct {
	Bom  string
	Text string
}

const utf8BOM = "\xEF\xBB\xBF"

// StripBom removes a UTF-8 byte-order-mark from the start of content.
func StripBom(content string) BomResult {
	if strings.HasPrefix(content, utf8BOM) {
		return BomResult{Bom: utf8BOM, Text: content[len(utf8BOM):]}
	}
	return BomResult{Text: content}
}

// ---- fuzzy matching ----

// NormalizeForFuzzyMatch applies the progressive normalizations from pi so
// that search-and-replace works across smart-quote and unicode-dash drift.
// Specifically:
//  1. Strip trailing whitespace per line
//  2. Smart quotes → ASCII
//  3. Unicode dashes → ASCII hyphen
//  4. Special spaces (NBSP etc.) → regular space
func NormalizeForFuzzyMatch(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	text = strings.Join(lines, "\n")

	repl := strings.NewReplacer(
		"\u201c", `"`, // U+201C LEFT DOUBLE QUOTATION MARK
		"\u201d", `"`, // U+201D RIGHT DOUBLE QUOTATION MARK
		"\u2018", "'", // U+2018 LEFT SINGLE QUOTATION MARK
		"\u2019", "'", // U+2019 RIGHT SINGLE QUOTATION MARK
		"\u201a", "'", // U+201A SINGLE LOW-9 QUOTATION MARK
		"\u201e", `"`, // U+201E DOUBLE LOW-9 QUOTATION MARK
		"\u2013", "-", // U+2013 EN DASH
		"\u2014", "-", // U+2014 EM DASH
		"\u2212", "-", // U+2212 MINUS SIGN
		"\u2010", "-", // U+2010 HYPHEN
		"\u2011", "-", // U+2011 NON-BREAKING HYPHEN
		"\u00a0", " ", // U+00A0 NO-BREAK SPACE
		"\u2000", " ", // U+2000 EN QUAD
		"\u2001", " ", // U+2001 EM QUAD
		"\u2002", " ", // U+2002 EN SPACE
		"\u2003", " ", // U+2003 EM SPACE
		"\u2009", " ", // U+2009 THIN SPACE
		"\u202f", " ", // U+202F NARROW NO-BREAK SPACE
	)
	return repl.Replace(text)
}

// FuzzyMatchResult is the outcome of FuzzyFindText.
type FuzzyMatchResult struct {
	Found                 bool
	Index                 int
	ContentForReplacement string
}

// FuzzyFindText tries an exact match first and, failing that, attempts a
// normalized match. When the normalized match succeeds the returned
// ContentForReplacement is the normalized content so subsequent edits
// operate on the same representation.
func FuzzyFindText(content, oldText string) FuzzyMatchResult {
	if idx := strings.Index(content, oldText); idx >= 0 {
		return FuzzyMatchResult{Found: true, Index: idx, ContentForReplacement: content}
	}
	normContent := NormalizeForFuzzyMatch(content)
	normOld := NormalizeForFuzzyMatch(oldText)
	if idx := strings.Index(normContent, normOld); idx >= 0 {
		return FuzzyMatchResult{Found: true, Index: idx, ContentForReplacement: normContent}
	}
	return FuzzyMatchResult{Found: false}
}

// ---- multi-edit application ----

// EditPair is a search-and-replace entry.
type EditPair struct {
	OldText string `json:"oldText"`
	NewText string `json:"newText"`
}

// EditResult is the output of ApplyEdits.
type EditResult struct {
	BaseContent string
	NewContent  string
}

// ApplyEdits applies every EditPair against the original content. Returns
// an error if any oldText is empty, missing, ambiguous, overlaps another
// match, or produces no net change.
func ApplyEdits(normalizedContent string, edits []EditPair, filePath string) (EditResult, error) {
	if len(edits) == 0 {
		return EditResult{}, fmt.Errorf("edit: no edits supplied")
	}
	for i, e := range edits {
		if e.OldText == "" {
			return EditResult{}, fmt.Errorf("edit %d: oldText is empty", i+1)
		}
	}

	// Decide whether to run in fuzzy (normalized) or exact mode.
	base := normalizedContent
	needsFuzzy := false
	for _, e := range edits {
		if !strings.Contains(base, e.OldText) {
			needsFuzzy = true
			break
		}
	}
	if needsFuzzy {
		normBase := NormalizeForFuzzyMatch(normalizedContent)
		allFuzzy := true
		for _, e := range edits {
			if !strings.Contains(normBase, NormalizeForFuzzyMatch(e.OldText)) {
				allFuzzy = false
				break
			}
		}
		if allFuzzy {
			base = normBase
		}
	}

	type match struct {
		index   int
		length  int
		newText string
		editIdx int
	}
	matches := make([]match, 0, len(edits))

	for i, e := range edits {
		old := e.OldText
		if needsFuzzy {
			old = NormalizeForFuzzyMatch(e.OldText)
		}
		first := strings.Index(base, old)
		if first < 0 {
			return EditResult{}, fmt.Errorf("edit %d in %s: oldText not found", i+1, filePath)
		}
		second := strings.Index(base[first+1:], old)
		if second >= 0 {
			return EditResult{}, fmt.Errorf("edit %d in %s: oldText is not unique", i+1, filePath)
		}
		matches = append(matches, match{
			index:   first,
			length:  len(old),
			newText: e.NewText,
			editIdx: i,
		})
	}

	sort.Slice(matches, func(i, j int) bool { return matches[i].index < matches[j].index })
	for i := 1; i < len(matches); i++ {
		if matches[i-1].index+matches[i-1].length > matches[i].index {
			return EditResult{}, fmt.Errorf("edits %d and %d in %s overlap", matches[i-1].editIdx+1, matches[i].editIdx+1, filePath)
		}
	}

	// Apply in reverse order so indices stay valid.
	out := base
	for i := len(matches) - 1; i >= 0; i-- {
		m := matches[i]
		out = out[:m.index] + m.newText + out[m.index+m.length:]
	}

	if out == base {
		return EditResult{}, fmt.Errorf("edit in %s: no changes", filePath)
	}
	return EditResult{BaseContent: base, NewContent: out}, nil
}

// ---- diff generation ----

// DiffResult is the return type of GenerateDiff.
type DiffResult struct {
	Diff             string
	FirstChangedLine *int
}

// GenerateDiff returns a pi-style unified diff string where each line is
// prefixed with its number plus '+' (add), '-' (remove) or ' ' (context).
func GenerateDiff(oldContent, newContent string, contextLines int) DiffResult {
	if contextLines < 0 {
		contextLines = 0
	}
	oldLines := splitLinesPreserving(oldContent)
	newLines := splitLinesPreserving(newContent)

	ops := lcsDiff(oldLines, newLines)

	var b strings.Builder
	var firstChanged *int

	// Identify change regions, then expand each with contextLines of context
	// on either side.
	type region struct{ from, to int } // indices into ops
	var regions []region
	for i := 0; i < len(ops); i++ {
		if ops[i].kind == ' ' {
			continue
		}
		j := i
		for j < len(ops) && ops[j].kind != ' ' {
			j++
		}
		regions = append(regions, region{from: i, to: j - 1})
		i = j - 1
	}

	if len(regions) == 0 {
		return DiffResult{Diff: ""}
	}

	// Expand with context and merge nearby regions.
	var merged []region
	for _, r := range regions {
		start := r.from - contextLines
		if start < 0 {
			start = 0
		}
		end := r.to + contextLines
		if end >= len(ops) {
			end = len(ops) - 1
		}
		if len(merged) > 0 && start <= merged[len(merged)-1].to+1 {
			merged[len(merged)-1].to = end
		} else {
			merged = append(merged, region{from: start, to: end})
		}
	}

	for ri, r := range merged {
		if ri > 0 {
			b.WriteString("...\n")
		}
		for i := r.from; i <= r.to; i++ {
			op := ops[i]
			if firstChanged == nil && op.kind != ' ' {
				lineNo := op.newLine
				if op.kind == '-' {
					lineNo = op.oldLine
				}
				l := lineNo
				firstChanged = &l
			}
			var lineNum int
			switch op.kind {
			case '+':
				lineNum = op.newLine
			case '-':
				lineNum = op.oldLine
			default:
				lineNum = op.newLine
			}
			fmt.Fprintf(&b, "%c%4d %s\n", op.kind, lineNum, op.text)
		}
	}

	return DiffResult{Diff: b.String(), FirstChangedLine: firstChanged}
}

// splitLinesPreserving returns lines without separators. An empty string
// returns nil so "no content" doesn't appear as a single empty line.
func splitLinesPreserving(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// diffOp is a single line in the computed diff.
type diffOp struct {
	kind    rune // '+' add, '-' remove, ' ' context
	text    string
	oldLine int
	newLine int
}

// lcsDiff computes a line-level diff using longest common subsequence.
func lcsDiff(a, b []string) []diffOp {
	n, m := len(a), len(b)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	var ops []diffOp
	i, j := 0, 0
	for i < n && j < m {
		if a[i] == b[j] {
			ops = append(ops, diffOp{kind: ' ', text: a[i], oldLine: i + 1, newLine: j + 1})
			i++
			j++
		} else if lcs[i+1][j] >= lcs[i][j+1] {
			ops = append(ops, diffOp{kind: '-', text: a[i], oldLine: i + 1})
			i++
		} else {
			ops = append(ops, diffOp{kind: '+', text: b[j], newLine: j + 1})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, diffOp{kind: '-', text: a[i], oldLine: i + 1})
	}
	for ; j < m; j++ {
		ops = append(ops, diffOp{kind: '+', text: b[j], newLine: j + 1})
	}
	return ops
}

// ---- guards ----

// ensure unicode package is referenced (used by fuzzy helpers indirectly)
var _ = utf8.RuneLen
