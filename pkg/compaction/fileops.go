package compaction

import (
	"sort"
	"strings"

	"github.com/gmoigneu/gcode/pkg/agent"
	"github.com/gmoigneu/gcode/pkg/ai"
)

// FileOperations tracks files touched during a conversation, split by the
// tool that touched them. A file may appear in multiple sets (e.g. read
// before being edited); ComputeFileLists resolves this into disjoint
// read-only vs modified sets.
type FileOperations struct {
	Read    map[string]bool
	Written map[string]bool
	Edited  map[string]bool
}

// NewFileOperations returns an empty FileOperations ready for use.
func NewFileOperations() *FileOperations {
	return &FileOperations{
		Read:    map[string]bool{},
		Written: map[string]bool{},
		Edited:  map[string]bool{},
	}
}

// ExtractFileOpsFromMessage scans a single assistant message for tool
// calls to read/write/edit and records the "path" argument of each.
// Non-assistant messages and tool calls without a path arg are ignored.
func ExtractFileOpsFromMessage(msg agent.AgentMessage) *FileOperations {
	ops := NewFileOperations()
	am, ok := msg.(*ai.AssistantMessage)
	if !ok {
		return ops
	}
	for _, c := range am.Content {
		tc, ok := c.(*ai.ToolCall)
		if !ok {
			continue
		}
		path, _ := tc.Arguments["path"].(string)
		if path == "" {
			continue
		}
		switch tc.Name {
		case "read":
			ops.Read[path] = true
		case "write":
			ops.Written[path] = true
		case "edit":
			ops.Edited[path] = true
		}
	}
	return ops
}

// ExtractFileOpsFromMessages accumulates file operations across a message
// list. Non-assistant messages are silently skipped.
func ExtractFileOpsFromMessages(messages []agent.AgentMessage) *FileOperations {
	ops := NewFileOperations()
	for _, m := range messages {
		ops.Merge(ExtractFileOpsFromMessage(m))
	}
	return ops
}

// Merge folds another FileOperations into this one. Both operands must be
// non-nil.
func (f *FileOperations) Merge(other *FileOperations) {
	if other == nil || f == nil {
		return
	}
	for k := range other.Read {
		f.Read[k] = true
	}
	for k := range other.Written {
		f.Written[k] = true
	}
	for k := range other.Edited {
		f.Edited[k] = true
	}
}

// ComputeFileLists resolves the tracked paths into a disjoint pair of
// (read-only, modified) slices, sorted alphabetically. A file that was
// read AND modified appears only in the modified slice.
func (f *FileOperations) ComputeFileLists() (readOnly []string, modified []string) {
	if f == nil {
		return nil, nil
	}
	modSet := make(map[string]bool, len(f.Written)+len(f.Edited))
	for k := range f.Written {
		modSet[k] = true
	}
	for k := range f.Edited {
		modSet[k] = true
	}
	for k := range f.Read {
		if !modSet[k] {
			readOnly = append(readOnly, k)
		}
	}
	for k := range modSet {
		modified = append(modified, k)
	}
	sort.Strings(readOnly)
	sort.Strings(modified)
	return
}

// FormatFileOperations renders the file lists as XML tags for inclusion in
// a compaction summary. Returns the empty string when both lists are empty.
func FormatFileOperations(readOnly, modified []string) string {
	if len(readOnly) == 0 && len(modified) == 0 {
		return ""
	}
	var b strings.Builder
	if len(readOnly) > 0 {
		b.WriteString("<read-files>\n")
		for _, f := range readOnly {
			b.WriteString(f)
			b.WriteByte('\n')
		}
		b.WriteString("</read-files>\n")
	}
	if len(modified) > 0 {
		b.WriteString("<modified-files>\n")
		for _, f := range modified {
			b.WriteString(f)
			b.WriteByte('\n')
		}
		b.WriteString("</modified-files>\n")
	}
	return b.String()
}
