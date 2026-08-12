package store

import (
	"encoding/json"
	"fmt"
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
	return &drawer.Drawer{
		SchemaVersion: drawer.SchemaVersion,
		Name:          "research-deck",
		DrawerKind:    drawer.DrawerKindFlow,
		Description:   "Research a topic and create an open-slide deck (https://open-slide.dev/).",
		Version:       "1",
		Flow: &drawer.FlowDefinition{
			InitialStage: "brief",
			Provider:     "local",
			Manager: drawer.FlowManager{
				Agent:        "activation.manager",
				SystemPrompt: "Supervise research-to-deck work. Prefer grounded claims, clear narrative, and open-slide (https://open-slide.dev/) as the deck format.",
			},
			Stages: []drawer.FlowStage{
				{
					ID: "brief", Title: "Brief",
					SystemPrompt:   "Capture audience, goal, length, tone, and success criteria for the deck. Advance to research when the brief is clear.",
					Worker:         &drawer.FlowWorker{Agent: "activation.worker"},
					Transitions:    []string{"research", "done"},
					TimeoutSeconds: 1800,
				},
				{
					ID: "research", Title: "Research",
					SystemPrompt:   "Gather sources and talking points. Record key claims with citations in the task body/comments. Advance to outline when research is sufficient.",
					Worker:         &drawer.FlowWorker{Agent: "activation.worker"},
					Transitions:    []string{"outline", "brief"},
					TimeoutSeconds: 3600,
				},
				{
					ID: "outline", Title: "Outline",
					SystemPrompt:   "Propose slide titles and section order before writing code. Advance to scaffold when the outline is locked.",
					Worker:         &drawer.FlowWorker{Agent: "activation.worker"},
					Transitions:    []string{"scaffold", "research"},
					TimeoutSeconds: 1800,
				},
				{
					ID: "scaffold", Title: "Scaffold",
					SystemPrompt:   "Create the open-slide workspace with `npx @open-slide/cli init` (or equivalent). Record the deck path on the task. Advance to draft when the workspace exists.",
					Worker:         &drawer.FlowWorker{Agent: "activation.worker"},
					Transitions:    []string{"draft", "outline"},
					TimeoutSeconds: 1800,
				},
				{
					ID: "draft", Title: "Draft",
					SystemPrompt:   "Author React pages for the deck (open-slide /create-slide style). One idea per slide where possible. Advance to review when a full first draft exists.",
					Worker:         &drawer.FlowWorker{Agent: "activation.worker"},
					Transitions:    []string{"review"},
					TimeoutSeconds: 3600,
				},
				{
					ID: "review", Title: "Review",
					SystemPrompt:   "Human review gate. Reviewer leaves open-slide comments or approves. Do not invent approvals.",
					Transitions:    []string{"polish", "draft"},
					Gate:           &drawer.FlowGate{RequiresHumanApproval: true},
					TimeoutSeconds: 86400,
				},
				{
					ID: "polish", Title: "Polish",
					SystemPrompt:   "Apply open-slide comments (/apply-comment or equivalent). Advance to done when feedback is addressed.",
					Worker:         &drawer.FlowWorker{Agent: "activation.worker"},
					Transitions:    []string{"done", "review"},
					TimeoutSeconds: 1800,
				},
				{ID: "done", Title: "Done", SystemPrompt: "Deck is ready to present or share."},
			},
		},
	}
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
		return builtin, nil
	}
	return append(builtin, primary...), nil
}

func (s PreferBuiltin) Fetch(name, version string) (*Bundle, error) {
	if strings.HasPrefix(name, "@buttonsflow/") {
		return (&BuiltinSource{}).Fetch(name, version)
	}
	if s.Primary == nil {
		return nil, fmt.Errorf("package %q not found", name)
	}
	return s.Primary.Fetch(name, version)
}
