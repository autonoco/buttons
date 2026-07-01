package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

	// alpha is stamped with source/source_name/version/content_hash
	a := installedSpec(t, home, "alpha")
	if a.Source != "local:test" || a.SourceName != "alpha" || a.Version != "1.0.0" || a.ContentHash == "" {
		t.Fatalf("alpha not stamped: source=%q source_name=%q version=%q hash=%q", a.Source, a.SourceName, a.Version, a.ContentHash)
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

// traversalSource is a Source whose Fetch returns a bundle file that escapes the
// install dir — exercises the install write-path containment check (#2).
type traversalSource struct{}

func (traversalSource) Index() ([]ButtonRef, error) { return nil, nil }
func (traversalSource) Fetch(name, _ string) (*Bundle, error) {
	bj, _ := json.Marshal(button.Button{SchemaVersion: 1, Name: name, Runtime: "shell"})
	return &Bundle{
		Name:   name,
		SHA256: "deadbeef",
		Files:  map[string][]byte{"button.json": bj, "../escape.sh": []byte("x")},
	}, nil
}

func TestFetchRejectsTraversalName(t *testing.T) {
	src := t.TempDir()
	writeSourceButton(t, src, button.Button{SchemaVersion: 1, Name: "ok", Runtime: "shell", Version: "1.0.0"})
	ls := &LocalSource{Root: src}
	for _, bad := range []string{"../escape", "..", ".", "a/b", "/abs"} {
		if _, err := ls.Fetch(bad, ""); err == nil {
			t.Errorf("Fetch(%q) should be rejected", bad)
		}
	}
	if _, err := ls.Fetch("ok", ""); err != nil {
		t.Errorf("Fetch(\"ok\") should succeed: %v", err)
	}
}

func TestFetchVersionMismatch(t *testing.T) {
	src := t.TempDir()
	writeSourceButton(t, src, button.Button{SchemaVersion: 1, Name: "pin", Runtime: "shell", Version: "1.0.0"})
	ls := &LocalSource{Root: src}
	if _, err := ls.Fetch("pin", "9.9.9"); err == nil {
		t.Error("Fetch with mismatched version pin should error")
	}
	if _, err := ls.Fetch("pin", "1.0.0"); err != nil {
		t.Errorf("Fetch with matching version pin should succeed: %v", err)
	}
	if _, err := ls.Fetch("pin", ""); err != nil {
		t.Errorf("Fetch with empty version (latest) should succeed: %v", err)
	}
}

func TestSafeJoin(t *testing.T) {
	dir := filepath.Clean("/tmp/install/alpha")
	for _, r := range []string{"../evil", "../../etc/passwd", "..", "/abs/path", "sub/../../escape"} {
		if _, err := safeJoin(dir, r); err == nil {
			t.Errorf("safeJoin(%q) should be rejected", r)
		}
	}
	for _, r := range []string{"button.json", "main.sh", "AGENTS.md", "sub/file.txt"} {
		dst, err := safeJoin(dir, r)
		if err != nil {
			t.Errorf("safeJoin(%q) should be allowed: %v", r, err)
		}
		if dst != dir && !strings.HasPrefix(dst, dir+string(filepath.Separator)) {
			t.Errorf("safeJoin(%q) escaped: %q", r, dst)
		}
	}
}

func TestInstallRejectsTraversalBundle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	if _, err := install(traversalSource{}, "evil", "", "local:test"); err == nil {
		t.Fatal("install should reject a bundle file that escapes the button dir")
	}
	// a rejected bundle must not leave a partial install behind
	if _, err := os.Stat(filepath.Join(home, "buttons", "evil")); !os.IsNotExist(err) {
		t.Fatal("rejected traversal install left a partial button dir behind")
	}
}

func TestFetchSkipsSymlinks(t *testing.T) {
	src := t.TempDir()
	writeSourceButton(t, src, button.Button{SchemaVersion: 1, Name: "ok", Runtime: "shell", Version: "1.0.0"})
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("top secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(src, "ok", "leak.txt")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	b, err := (&LocalSource{Root: src}).Fetch("ok", "")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if _, leaked := b.Files["leak.txt"]; leaked {
		t.Fatal("Fetch followed a symlink and leaked an out-of-root file")
	}
}
