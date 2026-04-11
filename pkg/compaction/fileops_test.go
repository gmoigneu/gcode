package compaction

import (
	"strings"
	"testing"

	"github.com/gmoigneu/gcode/pkg/agent"
	"github.com/gmoigneu/gcode/pkg/ai"
)

func toolCall(name, path string) *ai.ToolCall {
	return &ai.ToolCall{
		ID:        "c1",
		Name:      name,
		Arguments: map[string]any{"path": path},
	}
}

func assistantWithTools(calls ...*ai.ToolCall) *ai.AssistantMessage {
	content := make([]ai.Content, len(calls))
	for i, c := range calls {
		content[i] = c
	}
	return &ai.AssistantMessage{Content: content}
}

func TestExtractReadTool(t *testing.T) {
	ops := ExtractFileOpsFromMessage(assistantWithTools(toolCall("read", "/a.txt")))
	if !ops.Read["/a.txt"] {
		t.Error("read not tracked")
	}
	if len(ops.Written) != 0 || len(ops.Edited) != 0 {
		t.Error("other sets should be empty")
	}
}

func TestExtractWriteTool(t *testing.T) {
	ops := ExtractFileOpsFromMessage(assistantWithTools(toolCall("write", "/b.txt")))
	if !ops.Written["/b.txt"] {
		t.Error("write not tracked")
	}
}

func TestExtractEditTool(t *testing.T) {
	ops := ExtractFileOpsFromMessage(assistantWithTools(toolCall("edit", "/c.txt")))
	if !ops.Edited["/c.txt"] {
		t.Error("edit not tracked")
	}
}

func TestExtractNonAssistantReturnsEmpty(t *testing.T) {
	user := &ai.UserMessage{Content: []ai.Content{&ai.TextContent{Text: "hi"}}}
	ops := ExtractFileOpsFromMessage(user)
	if len(ops.Read)+len(ops.Written)+len(ops.Edited) != 0 {
		t.Error("non-assistant should have no file ops")
	}
}

func TestExtractMessagesAccumulates(t *testing.T) {
	msgs := []agent.AgentMessage{
		assistantWithTools(toolCall("read", "/a.txt")),
		assistantWithTools(toolCall("edit", "/a.txt"), toolCall("write", "/b.txt")),
	}
	ops := ExtractFileOpsFromMessages(msgs)
	if !ops.Read["/a.txt"] || !ops.Edited["/a.txt"] || !ops.Written["/b.txt"] {
		t.Errorf("ops = %+v", ops)
	}
}

func TestComputeFileListsReadOnlyVsModified(t *testing.T) {
	ops := NewFileOperations()
	ops.Read["/only-read.txt"] = true
	ops.Written["/written.txt"] = true
	ops.Edited["/edited.txt"] = true

	readOnly, modified := ops.ComputeFileLists()
	if len(readOnly) != 1 || readOnly[0] != "/only-read.txt" {
		t.Errorf("readOnly = %v", readOnly)
	}
	if len(modified) != 2 {
		t.Errorf("modified = %v", modified)
	}
	// Modified should be sorted.
	if modified[0] != "/edited.txt" || modified[1] != "/written.txt" {
		t.Errorf("modified not sorted: %v", modified)
	}
}

func TestComputeFileListsReadAndEditedGoesToModified(t *testing.T) {
	ops := NewFileOperations()
	ops.Read["/f.txt"] = true
	ops.Edited["/f.txt"] = true
	readOnly, modified := ops.ComputeFileLists()
	if len(readOnly) != 0 {
		t.Errorf("read-and-edited should not be read-only: %v", readOnly)
	}
	if len(modified) != 1 || modified[0] != "/f.txt" {
		t.Errorf("modified = %v", modified)
	}
}

func TestComputeFileListsNilSafe(t *testing.T) {
	var ops *FileOperations
	r, m := ops.ComputeFileLists()
	if r != nil || m != nil {
		t.Error("nil should return nils")
	}
}

func TestFormatFileOperationsEmpty(t *testing.T) {
	if FormatFileOperations(nil, nil) != "" {
		t.Error("empty lists should produce empty string")
	}
}

func TestFormatFileOperationsBothLists(t *testing.T) {
	got := FormatFileOperations([]string{"/a", "/b"}, []string{"/c"})
	if !strings.Contains(got, "<read-files>") || !strings.Contains(got, "/a") || !strings.Contains(got, "/b") {
		t.Errorf("read-files section missing: %q", got)
	}
	if !strings.Contains(got, "<modified-files>") || !strings.Contains(got, "/c") {
		t.Errorf("modified-files section missing: %q", got)
	}
}

func TestFormatFileOperationsReadOnly(t *testing.T) {
	got := FormatFileOperations([]string{"/only"}, nil)
	if !strings.Contains(got, "<read-files>") {
		t.Errorf("read-files missing: %q", got)
	}
	if strings.Contains(got, "<modified-files>") {
		t.Errorf("modified-files should be absent: %q", got)
	}
}

func TestMergeCombines(t *testing.T) {
	a := NewFileOperations()
	a.Read["/a"] = true
	b := NewFileOperations()
	b.Written["/b"] = true
	a.Merge(b)
	if !a.Read["/a"] || !a.Written["/b"] {
		t.Errorf("merge failed: %+v", a)
	}
}

func TestMergeNilIsNoop(t *testing.T) {
	a := NewFileOperations()
	a.Read["/a"] = true
	a.Merge(nil)
	if !a.Read["/a"] {
		t.Error("nil merge should not affect original")
	}
}

func TestExtractIgnoresToolCallWithoutPath(t *testing.T) {
	tc := &ai.ToolCall{Name: "read", Arguments: map[string]any{}}
	asst := &ai.AssistantMessage{Content: []ai.Content{tc}}
	ops := ExtractFileOpsFromMessage(asst)
	if len(ops.Read) != 0 {
		t.Errorf("ops = %+v", ops)
	}
}
