package store

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/autonoco/buttons/internal/drawer"
)

// BuiltinSource serves in-repo flow packages (e.g. @buttonsflow/research-deck)
// without a live registry. Used when BUTTONS_REGISTRY_URL is unset or as a fallback.
type BuiltinSource struct{}

func (s *BuiltinSource) Index() ([]ButtonRef, error) {
	return []ButtonRef{
		{Name: "@buttonsflow/research-deck", Kind: "drawer", Version: "1", Tags: []string{"flow", "research", "open-slide"}},
	}, nil
}

func (s *BuiltinSource) Fetch(name, version string) (*Bundle, error) {
	if name != "@buttonsflow/research-deck" {
		return nil, fmt.Errorf("package %q not found in builtin source", name)
	}
	if version != "" && version != "1" && version != "latest" {
		return nil, fmt.Errorf("package %q version %q not found", name, version)
	}
	d := researchDeckDrawer()
	raw, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, err
	}
	normalized, err := drawer.NormalizeFlowDefinition(d)
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{
		"drawer.json":          append(raw, '\n'),
		"flow-definition.json": normalized,
		"AGENTS.md": []byte(`# research-deck

Research a topic and author a slide deck with [open-slide](https://open-slide.dev/).

Stages: brief → research → outline → scaffold → draft → review → polish → done.

Scaffold uses ` + "`npx @open-slide/cli init`" + `; draft authors React pages; review uses open-slide comments; polish applies feedback.
`),
	}
	return &Bundle{
		Name:                 "research-deck",
		Kind:                 "drawer",
		Version:              "1",
		SHA256:               hashFiles(files),
		Files:                files,
		FlowDefinition:       normalized,
		FlowDefinitionSHA256: hashFiles(map[string][]byte{"flow-definition.json": normalized}),
	}, nil
}

func researchDeckDrawer() *drawer.Drawer {
	return drawer.ResearchDeckDrawer("local")
}

// PreferBuiltin wraps an optional primary Source and falls back to BuiltinSource
// for packages it does not have.
type PreferBuiltin struct {
	Primary Source
}

func (s PreferBuiltin) Index() ([]ButtonRef, error) {
	builtin, _ := (&BuiltinSource{}).Index()
	if s.Primary == nil {
		return builtin, nil
	}
	primary, err := s.Primary.Index()
	if err != nil {
		// Log so operators see why the primary source was skipped; fall
		// back to builtin intentionally so offline use still works.
		fmt.Fprintf(os.Stderr, "warning: primary source index failed, using builtin only: %v\n", err)
		return builtin, nil
	}
	return append(builtin, primary...), nil
}

func (s PreferBuiltin) Fetch(name, version string) (*Bundle, error) {
	if strings.HasPrefix(name, "@buttonsflow/") {
		b, err := (&BuiltinSource{}).Fetch(name, version)
		if err == nil {
			return b, nil
		}
		// Not found in builtin; fall through to Primary if available.
		if s.Primary != nil {
			return s.Primary.Fetch(name, version)
		}
		return nil, err
	}
	if s.Primary == nil {
		return nil, fmt.Errorf("package %q not found", name)
	}
	return s.Primary.Fetch(name, version)
}
