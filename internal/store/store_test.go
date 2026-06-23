package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/autonoco/buttons/internal/button"
)

// writeSourceButton writes a button folder into a LocalSource root.
func writeSourceButton(t *testing.T, root string, b button.Button) {
	t.Helper()
	dir := filepath.Join(root, b.Name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.MarshalIndent(&b, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "button.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.sh"), []byte("#!/bin/sh\necho hi\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

func installedSpec(t *testing.T, home, name string) button.Button {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, "buttons", name, "button.json"))
	if err != nil {
		t.Fatalf("button %q not installed: %v", name, err)
	}
	var b button.Button
	if err := json.Unmarshal(data, &b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestInstallByNameWithDeps(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	src := t.TempDir()
	writeSourceButton(t, src, button.Button{SchemaVersion: 1, Name: "alpha", Runtime: "shell", Version: "1.0.0", Tags: []string{"demo"}, Requires: []string{"beta"}})
	writeSourceButton(t, src, button.Button{SchemaVersion: 1, Name: "beta", Runtime: "shell", Version: "2.0.0", Tags: []string{"demo"}})

	res, err := InstallSpec(&LocalSource{Root: src}, "alpha", "local:test")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	got := append([]string{}, res.Installed...)
	sort.Strings(got)
	if len(got) != 2 || got[0] != "alpha" || got[1] != "beta" {
		t.Fatalf("want [alpha beta] (dep pulled), got %v", res.Installed)
	}

	// alpha is stamped with source/version/content_hash
	a := installedSpec(t, home, "alpha")
	if a.Source != "local:test" || a.Version != "1.0.0" || a.ContentHash == "" {
		t.Fatalf("alpha not stamped: source=%q version=%q hash=%q", a.Source, a.Version, a.ContentHash)
	}
	// dep beta installed too
	if b := installedSpec(t, home, "beta"); b.Version != "2.0.0" {
		t.Fatalf("beta version: %q", b.Version)
	}
	// code file got the exec bit
	info, err := os.Stat(filepath.Join(home, "buttons", "alpha", "main.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0100 == 0 {
		t.Fatalf("main.sh not executable: %v", info.Mode())
	}
}

func TestInstallByTag(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	src := t.TempDir()
	writeSourceButton(t, src, button.Button{SchemaVersion: 1, Name: "one", Runtime: "shell", Version: "1.0.0", Tags: []string{"grp"}})
	writeSourceButton(t, src, button.Button{SchemaVersion: 1, Name: "two", Runtime: "shell", Version: "1.0.0", Tags: []string{"grp"}})
	writeSourceButton(t, src, button.Button{SchemaVersion: 1, Name: "three", Runtime: "shell", Version: "1.0.0", Tags: []string{"other"}})

	res, err := InstallSpec(&LocalSource{Root: src}, "tag:grp", "local:test")
	if err != nil {
		t.Fatalf("install tag: %v", err)
	}
	got := append([]string{}, res.Installed...)
	sort.Strings(got)
	if len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("tag:grp should install [one two], got %v", res.Installed)
	}
	if _, err := os.Stat(filepath.Join(home, "buttons", "three")); !os.IsNotExist(err) {
		t.Fatalf("'three' (tag:other) should NOT be installed")
	}
}

func TestInstallTagNoMatch(t *testing.T) {
	t.Setenv("BUTTONS_HOME", t.TempDir())
	src := t.TempDir()
	writeSourceButton(t, src, button.Button{SchemaVersion: 1, Name: "x", Runtime: "shell"})
	if _, err := InstallSpec(&LocalSource{Root: src}, "tag:nope", "local:test"); err == nil {
		t.Fatal("expected error for tag with no matches")
	}
}

func TestSplitVersion(t *testing.T) {
	for _, c := range []struct{ in, name, ver string }{
		{"deploy", "deploy", ""},
		{"deploy@1.2.0", "deploy", "1.2.0"},
		{"@author/x", "@author/x", ""}, // leading @ is not a version sep
	} {
		n, v := splitVersion(c.in)
		if n != c.name || v != c.ver {
			t.Errorf("splitVersion(%q) = (%q,%q), want (%q,%q)", c.in, n, v, c.name, c.ver)
		}
	}
}
