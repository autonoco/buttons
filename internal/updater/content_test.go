package updater

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/store"
)

func writeSourceButton(t *testing.T, root string, b button.Button, body string) {
	t.Helper()
	dir := filepath.Join(root, b.Name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(&b, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "button.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.sh"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readInstalledButton(t *testing.T, home, name string) button.Button {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, "buttons", name, "button.json"))
	if err != nil {
		t.Fatal(err)
	}
	var b button.Button
	if err := json.Unmarshal(data, &b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestApplyContentUpdateFromLocalSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	src := t.TempDir()

	writeSourceButton(t, src, button.Button{SchemaVersion: 1, Name: "hello", Runtime: "shell", Version: "1.0.0"}, "#!/bin/sh\necho v1\n")
	if _, err := store.InstallSpec(&store.LocalSource{Root: src}, "hello", "local:"+src); err != nil {
		t.Fatalf("install v1: %v", err)
	}

	writeSourceButton(t, src, button.Button{SchemaVersion: 1, Name: "hello", Runtime: "shell", Version: "1.1.0"}, "#!/bin/sh\necho v2\n")
	report, err := Check(context.Background(), Options{SkipBinary: true})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(report.Buttons) != 1 || !report.Buttons[0].UpdateAvailable {
		t.Fatalf("expected one available content update, got %+v", report.Buttons)
	}

	result, err := Apply(context.Background(), Options{SkipBinary: true})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(result.UpdatedButtons) != 1 || result.UpdatedButtons[0] != "hello" {
		t.Fatalf("updated buttons = %v, want [hello]", result.UpdatedButtons)
	}
	got := readInstalledButton(t, home, "hello")
	if got.Version != "1.1.0" || got.SourceName != "hello" {
		t.Fatalf("installed version/source_name = %q/%q, want 1.1.0/hello", got.Version, got.SourceName)
	}
}

func TestCheckContentRequiresSourceName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	dir := filepath.Join(home, "buttons", "hello")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	b := button.Button{SchemaVersion: 1, Name: "hello", Runtime: "shell", Version: "1.0.0", Source: "local:/tmp/source"}
	data, _ := json.MarshalIndent(&b, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "button.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := Check(context.Background(), Options{SkipBinary: true})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(report.Buttons) != 1 || report.Buttons[0].Error == "" {
		t.Fatalf("expected source_name error, got %+v", report.Buttons)
	}
}

func TestApplyContentUpdateSkipsLocalEdits(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	src := t.TempDir()

	writeSourceButton(t, src, button.Button{SchemaVersion: 1, Name: "hello", Runtime: "shell", Version: "1.0.0"}, "#!/bin/sh\necho v1\n")
	if _, err := store.InstallSpec(&store.LocalSource{Root: src}, "hello", "local:"+src); err != nil {
		t.Fatalf("install v1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "buttons", "hello", "main.sh"), []byte("#!/bin/sh\necho local\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeSourceButton(t, src, button.Button{SchemaVersion: 1, Name: "hello", Runtime: "shell", Version: "1.1.0"}, "#!/bin/sh\necho v2\n")

	result, err := Apply(context.Background(), Options{SkipBinary: true})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(result.UpdatedButtons) != 0 {
		t.Fatalf("updated buttons = %v, want none", result.UpdatedButtons)
	}
	if len(result.Buttons) != 1 || !result.Buttons[0].Skipped {
		t.Fatalf("expected local edit skip, got %+v", result.Buttons)
	}
	got, err := os.ReadFile(filepath.Join(home, "buttons", "hello", "main.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "#!/bin/sh\necho local\n" {
		t.Fatalf("local edit was overwritten: %q", string(got))
	}
}
