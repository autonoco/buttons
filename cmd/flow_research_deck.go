package cmd

import (
	"fmt"

	"github.com/autonoco/buttons/internal/drawer"
	"github.com/autonoco/buttons/internal/flowkit"
)

// installResearchDeckBoard scaffolds the @buttonsflow/research-deck board locally.
func installResearchDeckBoard(provider string) error {
	if err := flowkit.EnsureButtons(provider); err != nil {
		return err
	}
	svc := drawer.NewService()
	if _, err := svc.Get("research-deck"); err == nil {
		return nil
	}
	if _, err := svc.CreateWithKind("research-deck", "Research a topic and create an open-slide deck (https://open-slide.dev/).", nil, drawer.DrawerKindFlow); err != nil {
		return err
	}
	stages := []struct{ id, title, prompt string }{
		{"brief", "Brief", "Capture audience, goal, length, tone, and success criteria for the deck."},
		{"research", "Research", "Gather sources and talking points with citations."},
		{"outline", "Outline", "Propose slide titles and section order."},
		{"scaffold", "Scaffold", "Create the open-slide workspace (`npx @open-slide/cli init`)."},
		{"draft", "Draft", "Author React pages for the deck."},
		{"review", "Review", "Human review gate via open-slide comments."},
		{"polish", "Polish", "Apply open-slide feedback."},
		{"done", "Done", "Deck ready to present or share."},
	}
	for _, s := range stages {
		if _, err := svc.AddFlowStage("research-deck", s.id, s.title, s.prompt); err != nil {
			return err
		}
	}
	sets := map[string]any{
		"initial_stage":                              "brief",
		"manager.agent":                              "activation.manager",
		"manager.system_prompt":                      "Supervise research-to-deck work using open-slide (https://open-slide.dev/).",
		"provider":                                   provider,
		"stages.brief.transitions":                   []string{"research", "done"},
		"stages.brief.worker.agent":                  "activation.worker",
		"stages.brief.timeout_seconds":               1800,
		"stages.research.transitions":                []string{"outline", "brief"},
		"stages.research.worker.agent":               "activation.worker",
		"stages.research.timeout_seconds":            3600,
		"stages.outline.transitions":                 []string{"scaffold", "research"},
		"stages.outline.worker.agent":                "activation.worker",
		"stages.outline.timeout_seconds":             1800,
		"stages.scaffold.transitions":                []string{"draft", "outline"},
		"stages.scaffold.worker.agent":               "activation.worker",
		"stages.scaffold.timeout_seconds":            1800,
		"stages.draft.transitions":                   []string{"review"},
		"stages.draft.worker.agent":                  "activation.worker",
		"stages.draft.timeout_seconds":               3600,
		"stages.review.transitions":                  []string{"polish", "draft"},
		"stages.review.gate.requires_human_approval": true,
		"stages.review.timeout_seconds":              86400,
		"stages.polish.transitions":                  []string{"done", "review"},
		"stages.polish.worker.agent":                 "activation.worker",
		"stages.polish.timeout_seconds":              1800,
	}
	for path, value := range sets {
		if _, err := svc.SetFlowField("research-deck", path, value); err != nil {
			return fmt.Errorf("set flow.%s: %w", path, err)
		}
	}
	return nil
}
