package ai

import (
	"reflect"
	"testing"
)

func TestParseStreamingJSONComplete(t *testing.T) {
	got := ParseStreamingJSON(`{"path":"/tmp/x","n":5}`)
	want := map[string]any{"path": "/tmp/x", "n": float64(5)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseStreamingJSONPartialObject(t *testing.T) {
	got := ParseStreamingJSON(`{"path":"/foo"`)
	want := map[string]any{"path": "/foo"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseStreamingJSONPartialKey(t *testing.T) {
	got := ParseStreamingJSON(`{"path":"/foo","off`)
	want := map[string]any{"path": "/foo"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseStreamingJSONNestedPartial(t *testing.T) {
	got := ParseStreamingJSON(`{"edits":[{"old`)
	// Accept either {"edits":[{}]} or {"edits":[]}: both are reasonable
	// fallbacks. We assert "edits" is present and is an array.
	edits, ok := got["edits"].([]any)
	if !ok {
		t.Fatalf("edits not an array: %v", got)
	}
	if len(edits) > 1 {
		t.Errorf("edits has too many elements: %v", edits)
	}
}

func TestParseStreamingJSONTrailingComma(t *testing.T) {
	got := ParseStreamingJSON(`{"a":1,`)
	want := map[string]any{"a": float64(1)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseStreamingJSONMidColon(t *testing.T) {
	got := ParseStreamingJSON(`{"a":1,"b":`)
	want := map[string]any{"a": float64(1)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseStreamingJSONEscapedString(t *testing.T) {
	got := ParseStreamingJSON(`{"msg":"hello \"world\""`)
	if got["msg"] != `hello "world"` {
		t.Errorf("msg = %v", got["msg"])
	}
}

func TestParseStreamingJSONNested(t *testing.T) {
	got := ParseStreamingJSON(`{"outer":{"inner":{"a":1`)
	outer, ok := got["outer"].(map[string]any)
	if !ok {
		t.Fatalf("outer not object: %v", got)
	}
	inner, ok := outer["inner"].(map[string]any)
	if !ok {
		t.Fatalf("inner not object: %v", outer)
	}
	if inner["a"] != float64(1) {
		t.Errorf("a = %v", inner["a"])
	}
}

func TestParseStreamingJSONPartialArray(t *testing.T) {
	got := ParseStreamingJSON(`{"items":[1,2,3`)
	items, ok := got["items"].([]any)
	if !ok {
		t.Fatalf("items not array: %v", got)
	}
	if len(items) != 3 {
		t.Errorf("items = %v", items)
	}
}

func TestParseStreamingJSONEmpty(t *testing.T) {
	got := ParseStreamingJSON("")
	if len(got) != 0 {
		t.Errorf("empty input should return empty map, got %v", got)
	}
	if got == nil {
		t.Error("empty input should return non-nil map")
	}
}

func TestParseStreamingJSONGarbage(t *testing.T) {
	got := ParseStreamingJSON("not valid json at all")
	if len(got) != 0 {
		t.Errorf("garbage should return empty map, got %v", got)
	}
}

func TestParseStreamingJSONPartialBoolean(t *testing.T) {
	got := ParseStreamingJSON(`{"ok":tru`)
	// "tru" is not a complete literal — should return empty (the open { mark).
	if _, has := got["ok"]; has {
		t.Errorf("partial boolean should be dropped: %v", got)
	}
}

func TestParseStreamingJSONTrueFalseNull(t *testing.T) {
	got := ParseStreamingJSON(`{"a":true,"b":false,"c":null}`)
	if got["a"] != true || got["b"] != false || got["c"] != nil {
		t.Errorf("got %v", got)
	}
}

func TestParseStreamingJSONNumbers(t *testing.T) {
	got := ParseStreamingJSON(`{"i":42,"f":3.14,"e":1.5e2,"n":-7}`)
	if got["i"] != float64(42) || got["f"] != float64(3.14) || got["e"] != float64(150) || got["n"] != float64(-7) {
		t.Errorf("got %v", got)
	}
}

func TestParseStreamingJSONWhitespace(t *testing.T) {
	got := ParseStreamingJSON("  \n{  \"a\" : 1 ,  \"b\" : 2 \n }")
	if got["a"] != float64(1) || got["b"] != float64(2) {
		t.Errorf("got %v", got)
	}
}

func TestParseStreamingJSONTopLevelArray(t *testing.T) {
	// Not an object: should degrade to empty map.
	got := ParseStreamingJSON("[1,2,3]")
	if len(got) != 0 {
		t.Errorf("top-level array should yield empty map, got %v", got)
	}
}

func TestParseStreamingJSONNull(t *testing.T) {
	got := ParseStreamingJSON("null")
	if len(got) != 0 {
		t.Errorf("null should yield empty map, got %v", got)
	}
	if got == nil {
		t.Error("should return non-nil map")
	}
}
