package cmd

import (
	"time"

	"github.com/autonoco/buttons/internal/drawer"
	"github.com/autonoco/buttons/internal/flowkit"
)

// installResearchDeckBoard scaffolds the @buttonsflow/research-deck board locally
// using the canonical definition from drawer.ResearchDeckDrawer.
func installResearchDeckBoard(provider string) error {
	if err := flowkit.EnsureButtons(provider); err != nil {
		return err
	}
	svc := drawer.NewService()
	if _, err := svc.Get("research-deck"); err == nil {
		return nil
	}
	// Create the scaffold (directory + AGENTS.md + pressed/).
	if _, err := svc.CreateWithKind("research-deck", "Research a topic and create an open-slide deck (https://open-slide.dev/).", nil, drawer.DrawerKindFlow); err != nil {
		return err
	}
	// Overwrite with the canonical definition; roll back the scaffold on failure
	// so a half-installed board is not left behind.
	d := drawer.ResearchDeckDrawer(provider)
	now := time.Now().UTC()
	d.CreatedAt = now
	d.UpdatedAt = now
	if err := svc.Save(d); err != nil {
		_ = svc.Remove("research-deck")
		return err
	}
	return nil
}
