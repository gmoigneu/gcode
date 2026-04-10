package ai

import (
	"encoding/json"
	"reflect"
	"testing"
)

type schemaSimple struct {
	Path   string `json:"path" description:"Path to the file"`
	Offset int    `json:"offset,omitempty" description:"Line offset"`
}

type schemaOptionalPtr struct {
	Path  string `json:"path"`
	Limit *int   `json:"limit" description:"Max lines"`
}

type schemaAllBasics struct {
	S  string  `json:"s"`
	I  int     `json:"i"`
	I8 int8    `json:"i8"`
	U  uint    `json:"u"`
	F  float64 `json:"f"`
	B  bool    `json:"b"`
}

type schemaSlices struct {
	Paths []string `json:"paths" description:"A list of paths"`
	Nums  []int    `json:"nums"`
}

type nestedInner struct {
	Name string `json:"name"`
	Age  int    `json:"age,omitempty"`
}

type schemaNested struct {
	Inner nestedInner `json:"inner"`
}

type schemaSkipped struct {
	Included string `json:"included"`
	Hidden   string `json:"-"`
	NoTag    string
}

func TestSchemaSimple(t *testing.T) {
	got := decodeSchema(t, SchemaFrom[schemaSimple]())

	if got["type"] != "object" {
		t.Errorf("type = %v", got["type"])
	}
	props := got["properties"].(map[string]any)
	path := props["path"].(map[string]any)
	if path["type"] != "string" || path["description"] != "Path to the file" {
		t.Errorf("path = %+v", path)
	}
	offset := props["offset"].(map[string]any)
	if offset["type"] != "integer" {
		t.Errorf("offset.type = %v", offset["type"])
	}
	req, _ := got["required"].([]any)
	if !containsString(req, "path") {
		t.Errorf("path should be required: %v", req)
	}
	if containsString(req, "offset") {
		t.Errorf("offset should not be required (omitempty): %v", req)
	}
}

func TestSchemaPointerOptional(t *testing.T) {
	got := decodeSchema(t, SchemaFrom[schemaOptionalPtr]())
	req, _ := got["required"].([]any)
	if !containsString(req, "path") {
		t.Errorf("path should be required: %v", req)
	}
	if containsString(req, "limit") {
		t.Errorf("limit (pointer) should not be required: %v", req)
	}
	props := got["properties"].(map[string]any)
	limit := props["limit"].(map[string]any)
	if limit["type"] != "integer" {
		t.Errorf("limit type = %v", limit["type"])
	}
}

func TestSchemaAllBasics(t *testing.T) {
	got := decodeSchema(t, SchemaFrom[schemaAllBasics]())
	props := got["properties"].(map[string]any)
	want := map[string]string{
		"s":  "string",
		"i":  "integer",
		"i8": "integer",
		"u":  "integer",
		"f":  "number",
		"b":  "boolean",
	}
	for k, typ := range want {
		p := props[k].(map[string]any)
		if p["type"] != typ {
			t.Errorf("%s.type = %v, want %s", k, p["type"], typ)
		}
	}
}

func TestSchemaSlices(t *testing.T) {
	got := decodeSchema(t, SchemaFrom[schemaSlices]())
	props := got["properties"].(map[string]any)
	paths := props["paths"].(map[string]any)
	if paths["type"] != "array" {
		t.Errorf("paths.type = %v", paths["type"])
	}
	items := paths["items"].(map[string]any)
	if items["type"] != "string" {
		t.Errorf("paths.items.type = %v", items["type"])
	}
	if paths["description"] != "A list of paths" {
		t.Errorf("paths.description = %v", paths["description"])
	}
}

func TestSchemaNested(t *testing.T) {
	got := decodeSchema(t, SchemaFrom[schemaNested]())
	props := got["properties"].(map[string]any)
	inner := props["inner"].(map[string]any)
	if inner["type"] != "object" {
		t.Errorf("inner.type = %v", inner["type"])
	}
	innerProps := inner["properties"].(map[string]any)
	name := innerProps["name"].(map[string]any)
	if name["type"] != "string" {
		t.Errorf("inner.name.type = %v", name["type"])
	}
	req, _ := inner["required"].([]any)
	if !containsString(req, "name") || containsString(req, "age") {
		t.Errorf("inner.required = %v (name required, age not)", req)
	}
}

func TestSchemaSkippedFields(t *testing.T) {
	got := decodeSchema(t, SchemaFrom[schemaSkipped]())
	props := got["properties"].(map[string]any)
	if _, ok := props["Hidden"]; ok {
		t.Error("fields with json:'-' should be skipped")
	}
	if _, ok := props["hidden"]; ok {
		t.Error("fields with json:'-' should be skipped")
	}
	if _, ok := props["included"]; !ok {
		t.Error("included field missing")
	}
	// Untagged exported field: should use the Go field name.
	if _, ok := props["NoTag"]; !ok {
		t.Error("untagged field should be included under its Go name")
	}
}

func TestSchemaNotStructReturnsEmptyObject(t *testing.T) {
	got := decodeSchema(t, SchemaFrom[int]())
	if got["type"] != "object" {
		t.Errorf("expected fallback type=object, got %v", got)
	}
}

func TestSchemaNoRequiredOmitted(t *testing.T) {
	// If every field is optional, "required" should not appear (or be empty).
	type allOptional struct {
		A *string `json:"a"`
		B int     `json:"b,omitempty"`
	}
	got := decodeSchema(t, SchemaFrom[allOptional]())
	if req, ok := got["required"]; ok {
		arr := req.([]any)
		if len(arr) != 0 {
			t.Errorf("required = %v, want empty", arr)
		}
	}
}

func decodeSchema(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("schema is not valid JSON: %v; raw=%s", err, string(raw))
	}
	return m
}

func containsString(xs []any, want string) bool {
	for _, x := range xs {
		if s, ok := x.(string); ok && s == want {
			return true
		}
	}
	return false
}

// Ensure reflect is still referenced to keep lints happy even if the test
// suite stops using DeepEqual.
var _ = reflect.DeepEqual
