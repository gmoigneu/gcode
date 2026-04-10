package ai

import (
	"encoding/json"
	"reflect"
	"strings"
)

// SchemaFrom generates a JSON Schema fragment from the Go type T. It is
// intentionally lightweight — only enough to describe tool parameter structs.
//
// Supported Go kinds:
//   - string                       -> {"type":"string"}
//   - int*, uint*                  -> {"type":"integer"}
//   - float32, float64             -> {"type":"number"}
//   - bool                         -> {"type":"boolean"}
//   - []T                          -> {"type":"array","items":<schema of T>}
//   - *T                           -> schema of T (field is optional)
//   - struct                       -> {"type":"object","properties":...,"required":[...]}
//
// Struct fields use the `json` tag for the property name. Fields tagged
// `json:"-"` are skipped. `omitempty` and pointer types make a field optional
// (not present in the `required` array). The `description` struct tag is
// copied into the schema as the field's description.
//
// If T is not a struct the result is an empty-object schema.
func SchemaFrom[T any]() json.RawMessage {
	var zero T
	t := reflect.TypeOf(zero)
	schema := typeToSchema(t)
	if schema == nil || schema["type"] != "object" {
		schema = map[string]any{"type": "object"}
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage(`{"type":"object"}`)
	}
	return raw
}

// typeToSchema returns a schema fragment for t. Pointer types are
// dereferenced. Unsupported kinds yield a nil map (caller decides the fallback).
func typeToSchema(t reflect.Type) map[string]any {
	if t == nil {
		return nil
	}
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Slice, reflect.Array:
		items := typeToSchema(t.Elem())
		if items == nil {
			items = map[string]any{}
		}
		return map[string]any{"type": "array", "items": items}
	case reflect.Struct:
		return structToSchema(t)
	default:
		return nil
	}
}

func structToSchema(t reflect.Type) map[string]any {
	props := map[string]any{}
	required := []string{}

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, omitempty, skip := parseJSONTag(f)
		if skip {
			continue
		}

		fieldSchema := typeToSchema(f.Type)
		if fieldSchema == nil {
			fieldSchema = map[string]any{}
		}
		if desc := f.Tag.Get("description"); desc != "" {
			fieldSchema["description"] = desc
		}
		props[name] = fieldSchema

		optional := omitempty || f.Type.Kind() == reflect.Ptr
		if !optional {
			required = append(required, name)
		}
	}

	out := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

// parseJSONTag returns (name, omitempty, skip).
func parseJSONTag(f reflect.StructField) (string, bool, bool) {
	tag := f.Tag.Get("json")
	if tag == "-" {
		return "", false, true
	}
	if tag == "" {
		return f.Name, false, false
	}
	parts := strings.Split(tag, ",")
	name := parts[0]
	if name == "" {
		name = f.Name
	}
	omitempty := false
	for _, p := range parts[1:] {
		if p == "omitempty" {
			omitempty = true
		}
	}
	return name, omitempty, false
}
