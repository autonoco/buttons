// Package docscheck holds guards that keep the Mintlify docs (docs/) in sync
// with the code. It has no runtime code — only tests run by `go test ./...`.
package docscheck

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// docsRoot is the docs/ directory relative to this package's directory.
const docsRoot = "../../docs"

// collectNavPages walks docs.json's navigation tree and returns every page ref
// (the string entries inside any "pages" array).
func collectNavPages(t *testing.T) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(docsRoot, "docs.json"))
	if err != nil {
		t.Fatalf("read docs.json: %v", err)
	}
	var cfg any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse docs.json: %v", err)
	}
	pages := map[string]bool{}
	var walk func(v any)
	walk = func(v any) {
		switch n := v.(type) {
		case map[string]any:
			for k, val := range n {
				if k == "pages" {
					if arr, ok := val.([]any); ok {
						for _, p := range arr {
							if s, ok := p.(string); ok {
								pages[s] = true
							} else {
								walk(p)
							}
						}
						continue
					}
				}
				walk(val)
			}
		case []any:
			for _, x := range n {
				walk(x)
			}
		}
	}
	walk(cfg)
	return pages
}

// TestEveryCLIPageInNav guards against the docs-sync workflow generating a new
// docs/cli/*.md page (from a new Cobra command) without wiring it into
// docs.json navigation — which leaves the command undiscoverable in the docs
// sidebar. If this fails, add the listed page(s) to a group in the "CLI
// reference" tab of docs/docs.json.
func TestEveryCLIPageInNav(t *testing.T) {
	nav := collectNavPages(t)
	entries, err := os.ReadDir(filepath.Join(docsRoot, "cli"))
	if err != nil {
		t.Fatalf("read docs/cli: %v", err)
	}
	var missing []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		page := "cli/" + strings.TrimSuffix(e.Name(), ".md")
		if !nav[page] {
			missing = append(missing, page)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("CLI reference pages missing from docs/docs.json navigation "+
			"(add each to a group in the \"CLI reference\" tab): %s", strings.Join(missing, ", "))
	}
}
