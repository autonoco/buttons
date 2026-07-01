package cmd

import (
	"testing"

	"github.com/autonoco/buttons/internal/button"
	"github.com/spf13/cobra"
)

func TestCompleteButtonNamesFiltersByPrefix(t *testing.T) {
	t.Setenv("BUTTONS_HOME", t.TempDir())
	svc := button.NewService()
	if _, err := svc.Create(button.CreateOpts{Name: "deploy", Code: "echo deploy", Runtime: "shell", TimeoutSeconds: 10}); err != nil {
		t.Fatalf("create deploy: %v", err)
	}
	if _, err := svc.Create(button.CreateOpts{Name: "ship", Code: "echo ship", Runtime: "shell", TimeoutSeconds: 10}); err != nil {
		t.Fatalf("create ship: %v", err)
	}

	got, directive := completeButtonNames(&cobra.Command{}, nil, "dep")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("directive = %v, want NoFileComp", directive)
	}
	if len(got) != 1 || got[0] != "deploy" {
		t.Fatalf("completions = %#v, want [deploy]", got)
	}
}
