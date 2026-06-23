package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/autonoco/buttons/internal/button"
)

// bumpSourceButton rewrites a source button's version and main.sh so its
// content hash changes — simulating an upstream release.
func bumpSourceButton(t *testing.T, root, name, version, body string) {
	t.Helper()
	dir := filepath.Join(root, name)
	b := button.Button{SchemaVersion: 1, Name: name, Runtime: "shell", Version: version, Tags: []string{"demo"}}
	data, _ := json.MarshalIndent(&b, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "button.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.sh"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateDetectsAndAppliesDrift(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	src := t.TempDir()
	writeSourceButton(t, src, button.Button{SchemaVersion: 1, Name: "alpha", Runtime: "shell", Version: "1.0.0", Tags: []string{"demo"}})

	srcRef := "local:" + src
	if _, err := InstallSpec(&LocalSource{Root: src}, "alpha", srcRef); err != nil {
		t.Fatalf("install: %v", err)
	}

	// No upstream change yet → unchanged.
	res, err := UpdateInstalled(DefaultSourceResolver, false)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if len(res.Buttons) != 1 || res.Buttons[0].Action != "unchanged" {
		t.Fatalf("want [unchanged], got %+v", res.Buttons)
	}

	// Bump the source → check reports it available without writing.
	bumpSourceButton(t, src, "alpha", "2.0.0", "#!/bin/sh\necho v2\n")
	res, err = UpdateInstalled(DefaultSourceResolver, true)
	if err != nil {
		t.Fatalf("update --check: %v", err)
	}
	if res.Buttons[0].Action != "available" || res.Buttons[0].To != "2.0.0" {
		t.Fatalf("want available→2.0.0, got %+v", res.Buttons[0])
	}
	if got := installedSpec(t, home, "alpha").Version; got != "1.0.0" {
		t.Fatalf("--check must not write; installed version = %q", got)
	}

	// Apply → updated, and the installed copy is now v2 with a fresh hash.
	before := installedSpec(t, home, "alpha")
	res, err = UpdateInstalled(DefaultSourceResolver, false)
	if err != nil {
		t.Fatalf("update apply: %v", err)
	}
	if res.Buttons[0].Action != "updated" || res.Buttons[0].From != "1.0.0" || res.Buttons[0].To != "2.0.0" {
		t.Fatalf("want updated 1.0.0→2.0.0, got %+v", res.Buttons[0])
	}
	after := installedSpec(t, home, "alpha")
	if after.Version != "2.0.0" {
		t.Fatalf("installed version after update = %q", after.Version)
	}
	if after.ContentHash == before.ContentHash || after.ContentHash == "" {
		t.Fatalf("content hash should change on update: before=%q after=%q", before.ContentHash, after.ContentHash)
	}
	body, _ := os.ReadFile(filepath.Join(home, "buttons", "alpha", "main.sh"))
	if string(body) != "#!/bin/sh\necho v2\n" {
		t.Fatalf("main.sh not refreshed: %q", body)
	}

	// A second run is now a no-op.
	res, _ = UpdateInstalled(DefaultSourceResolver, false)
	if res.Buttons[0].Action != "unchanged" {
		t.Fatalf("re-run should be unchanged, got %+v", res.Buttons[0])
	}
}

func TestUpdateSkipsUnsourcedAndUnresolvable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)

	// A locally-created button (no source) lands directly in the buttons dir.
	local := filepath.Join(home, "buttons", "handmade")
	if err := os.MkdirAll(local, 0755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.MarshalIndent(&button.Button{SchemaVersion: 1, Name: "handmade", Runtime: "shell"}, "", "  ")
	if err := os.WriteFile(filepath.Join(local, "button.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	// A button stamped with a registry source we can't resolve yet.
	reg := filepath.Join(home, "buttons", "remote")
	if err := os.MkdirAll(reg, 0755); err != nil {
		t.Fatal(err)
	}
	data, _ = json.MarshalIndent(&button.Button{SchemaVersion: 1, Name: "remote", Runtime: "shell", Source: "https://buttons.co", ContentHash: "abc"}, "", "  ")
	if err := os.WriteFile(filepath.Join(reg, "button.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	res, err := UpdateInstalled(DefaultSourceResolver, false)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	byName := map[string]UpdateStatus{}
	for _, b := range res.Buttons {
		byName[b.Name] = b
	}
	if byName["handmade"].Action != "skipped" {
		t.Fatalf("handmade should be skipped, got %+v", byName["handmade"])
	}
	if byName["remote"].Action != "skipped" {
		t.Fatalf("remote (registry source) should be skipped until #275, got %+v", byName["remote"])
	}
}

func TestDefaultSourceResolver(t *testing.T) {
	if _, err := DefaultSourceResolver("local:/tmp/pack"); err != nil {
		t.Fatalf("local: should resolve, got %v", err)
	}
	if _, err := DefaultSourceResolver("https://buttons.co"); err == nil {
		t.Fatal("registry source should not resolve yet")
	}
}
