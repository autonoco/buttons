package cmd

import (
	"io"
	"testing"

	"github.com/autonoco/buttons/internal/settings"
)

func TestUpdaterOptionsReadsCLIAutoUpdateSetting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)

	svc := settings.NewService(home)
	if err := svc.Set(settings.KeyCLIAutoUpdate, "true"); err != nil {
		t.Fatalf("set cli-auto-update: %v", err)
	}

	opts := updaterOptions(io.Discard)
	if !opts.CLIAutoUpdate {
		t.Fatal("CLIAutoUpdate = false, want true")
	}
}
