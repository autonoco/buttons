// Command schemagen emits JSON Schema Draft 2020-12 documents for
// the Buttons entity types. The Go struct is the single source of
// truth; this tool produces the schemas that agents, IDEs, and LSP
// tools consume.
//
// Run via `go generate ./...` from the repo root. CI regenerates and
// fails the build on drift so the committed schema can never lie
// about the Go shape.
//
// We hand-roll the reflection walk (rather than depend on a third
// party library) to keep the dependency tree small. Covers the
// subset of JSON Schema we actually emit for drawer specs:
// objects, arrays, strings/ints/bools/numbers, enum (string const),
// required fields, descriptions from jsonschema:"description=..."
// struct tags, const values, nested $defs for named types.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/autonoco/buttons/internal/drawer"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "schemagen:", err)
		os.Exit(1)
	}
}

func run() error {
	publicPath := flagOr("-public", "docs/schemas/drawer.schema.json")
	embedPath := flagOr("-embed", "internal/drawer/schema_embedded.json")

	gen := newGenerator()
	root := gen.walk(reflect.TypeOf(drawer.Drawer{}))

	doc := map[string]any{
		"$schema":     "https://json-schema.org/draft/2020-12/schema",
		"$id":         "https://buttons.sh/schemas/drawer.schema.json",
		"title":       "Drawer",
		"description": "Buttons drawer: a typed workflow chaining buttons with ${ref} references between steps.",
	}
	// Merge root properties at the top level (ExpandedStruct-style)
	// so authors see a flat schema, not one wrapped in a $ref.
	for k, v := range root {
		doc[k] = v
	}
	if len(gen.defs) > 0 {
		doc["$defs"] = gen.defs
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	for _, p := range []string{publicPath, embedPath} {
		// #nosec G301 G306 -- build-time artifacts for public docs
		// (docs/schemas/*.json) and embedded assets; both must be
		// world-readable so editors, LLM tooling, and SchemaStore
		// can fetch them. No secrets — schemas are public.
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(p), err)
		}
		// #nosec G306 -- see rationale above.
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", p, err)
		}
		fmt.Printf("wrote %s\n", p)
	}
	return nil
}

// generator walks a Go type graph once and produces JSON Schema.
// Named struct types get extracted into $defs and referenced via
// $ref so recursive / repeated shapes don't duplicate.
type generator struct {
	defs map[string]any
}

func newGenerator() *generator {
	return &generator{defs: map[string]any{}}
}

// walk returns the JSON Schema for t, populating g.defs along the
// way. For named structs other than the root type, the schema is
// stashed in g.defs and a $ref is returned.
func (g *generator) walk(t reflect.Type) map[string]any {
	for t.Kind() == reflect.Pointer {
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
		return map[string]any{
			"type":  "array",
			"items": g.walk(t.Elem()),
		}
	case reflect.Map:
		return map[string]any{"type": "object"}
	case reflect.Interface:
		// any — accept anything.
		return map[string]any{}
	case reflect.Struct:
		// Special-case time.Time → string (date-time). Matches
		// how encoding/json serializes it by default.
		if t.PkgPath() == "time" && t.Name() == "Time" {
			return map[string]any{"type": "string", "format": "date-time"}
		}
		return g.structSchema(t)
	}
	return map[string]any{}
}

// structSchema builds the schema for a struct. Walks every exported
// field; honors `json:"..."` for field naming + omitempty, and
// `jsonschema:"..."` for enum/const/description/required overrides.
func (g *generator) structSchema(t reflect.Type) map[string]any {
	// If this is a named (non-anonymous) struct that isn't the root,
	// park it in $defs so repeated uses dedupe.
	if t.Name() != "" && t.Name() != "Drawer" {
		if _, seen := g.defs[t.Name()]; !seen {
			// Stash a placeholder first to break cycles, then fill it.
			g.defs[t.Name()] = map[string]any{}
			g.defs[t.Name()] = g.buildStruct(t)
		}
		return map[string]any{"$ref": "#/$defs/" + t.Name()}
	}
	return g.buildStruct(t)
}

func (g *generator) buildStruct(t reflect.Type) map[string]any {
	props := map[string]any{}
	required := []string{}

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}

		jsonTag := f.Tag.Get("json")
		name, omitEmpty := parseJSONTag(jsonTag)
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}

		// Walk the field's type first; may produce a $ref.
		fieldSchema := g.walk(f.Type)

		// Apply jsonschema tag overrides.
		tag := f.Tag.Get("jsonschema")
		applyJSONSchemaTag(fieldSchema, tag)

		props[name] = fieldSchema

		// A field is required when there's no omitempty AND the
		// jsonschema tag didn't explicitly opt out. We don't support
		// Go's "required_from_jsonschema_tags" semantics here; we
		// derive purely from the json tag.
		if !omitEmpty && f.Type.Kind() != reflect.Pointer {
			required = append(required, name)
		}
	}

	out := map[string]any{
		"type":                 "object",
		"properties":           props,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

// applyJSONSchemaTag mutates the schema map to honor tag directives
// like `jsonschema:"description=...,enum=a,enum=b,const=1"`.
func applyJSONSchemaTag(schema map[string]any, tag string) {
	if tag == "" {
		return
	}
	parts := splitTagRespectingEscapes(tag)
	var enums []any
	for _, p := range parts {
		kv := strings.SplitN(p, "=", 2)
		key := kv[0]
		val := ""
		if len(kv) == 2 {
			val = kv[1]
		}
		switch key {
		case "description":
			schema["description"] = val
		case "enum":
			enums = append(enums, val)
		case "const":
			// Numeric consts are stored as-is if they parse; else
			// string const.
			var num json.Number
			if err := json.Unmarshal([]byte(val), &num); err == nil {
				schema["const"] = jsonNumberToAny(num)
			} else {
				schema["const"] = val
			}
		case "title":
			schema["title"] = val
		case "format":
			schema["format"] = val
		case "pattern":
			schema["pattern"] = val
		}
	}
	if len(enums) > 0 {
		schema["enum"] = enums
	}
}

// splitTagRespectingEscapes splits a struct tag value on commas
// without breaking `\,` escapes. Our current tags don't use
// escapes, so a plain split is fine here.
func splitTagRespectingEscapes(tag string) []string {
	return strings.Split(tag, ",")
}

// parseJSONTag returns (fieldName, omitEmpty) parsed from a json:
// struct tag.
func parseJSONTag(tag string) (string, bool) {
	if tag == "" {
		return "", false
	}
	parts := strings.Split(tag, ",")
	name := parts[0]
	omit := false
	for _, opt := range parts[1:] {
		if opt == "omitempty" {
			omit = true
		}
	}
	return name, omit
}

// jsonNumberToAny preserves integer-ness when a numeric const is
// unambiguous.
func jsonNumberToAny(n json.Number) any {
	if i, err := n.Int64(); err == nil {
		return i
	}
	if f, err := n.Float64(); err == nil {
		return f
	}
	return n.String()
}

// flagOr returns the value after flag in os.Args, or fallback.
func flagOr(flag, fallback string) string {
	for i, a := range os.Args {
		if a == flag && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
	}
	return fallback
}
