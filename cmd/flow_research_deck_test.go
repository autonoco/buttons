package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/autonoco/buttons/internal/drawer"
)

func TestInstallResearchDeckBoardPreservesScaffoldAndExistingBoard(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	if err := installResearchDeckBoard("local"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"drawer.json", "AGENTS.md", "pressed"} {
		if _, err := os.Stat(filepath.Join(home, "drawers", "research-deck", name)); err != nil {
			t.Fatal(err)
		}
	}
	svc := drawer.NewService()
	d, err := svc.Get("research-deck")
	if err != nil {
		t.Fatal(err)
	}
	if d.Flow == nil || len(d.Flow.Stages) != len(drawer.ResearchDeckDrawer("local").Flow.Stages) {
		t.Fatalf("canonical flow was not installed: %+v", d.Flow)
	}
	d.Description = "Keep my customized research workflow"
	if err := svc.Save(d); err != nil {
		t.Fatal(err)
	}
	if err := installResearchDeckBoard("local"); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get("research-deck")
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != d.Description {
		t.Fatal("duplicate installation overwrote the existing board")
	}
}
