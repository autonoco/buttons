// Command schemagen emits JSON Schema documents for the Buttons
// entity types. The Go structs are the single source of truth; this
// tool produces the schemas that agents, IDEs, and LSP tools consume.
//
// Run via `go generate ./...` from the repo root. CI regenerates and
// fails the build on drift so the committed schema can never lie
// about the Go shape.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/invopop/jsonschema"

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

	r := &jsonschema.Reflector{
		ExpandedStruct:             true,
		AllowAdditionalProperties:  false,
		RequiredFromJSONSchemaTags: true,
	}
	schema := r.Reflect(&drawer.Drawer{})
	schema.ID = "https://buttons.sh/schemas/drawer.schema.json"
	schema.Title = "Drawer"
	schema.Description = "Buttons drawer: a typed workflow chaining buttons with ${ref} references between steps."

	data, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	for _, p := range []string{publicPath, embedPath} {
		// #nosec G301 G306 -- these are build-time artifacts for docs
		// (docs/schemas/*.json) and embedded assets; both need to be
		// world-readable so editors, LLM tooling, and SchemaStore can
		// fetch them. They carry no secrets — schemas are public by
		// definition. 0755/0644 is the correct permission set here.
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(p), err)
		}
		// #nosec G306 -- see rationale above; public schema artifact.
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", p, err)
		}
		fmt.Printf("wrote %s\n", p)
	}
	return nil
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
