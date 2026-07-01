package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/manifest"
)

func sourceBundle(t *testing.T, ref string, b button.Button, body string) *Bundle {
	t.Helper()
	files := map[string][]byte{}
	data, err := json.MarshalIndent(&b, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	files["button.json"] = data
	files["main.sh"] = []byte(body)
	return &Bundle{Name: ref, Version: b.Version, SHA256: hashFiles(files), Files: files}
}

type memorySource struct {
	refs    []ButtonRef
	bundles map[string]*Bundle
}

func (s memorySource) Index() ([]ButtonRef, error) {
	return s.refs, nil
}

func (s memorySource) Fetch(name, version string) (*Bundle, error) {
	if version == "" {
		for _, ref := range s.refs {
			if ref.Name == name {
				version = ref.Version
			}
		}
	}
	b, ok := s.bundles[name+"@"+version]
	if !ok {
		return nil, os.ErrNotExist
	}
	return b, nil
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

func TestInstallManifestWithDepsWritesLock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

	alpha := sourceBundle(t, "@autono/alpha", button.Button{
		SchemaVersion: 1,
		Name:          "alpha",
		Runtime:       "shell",
		Version:       "1.0.0",
		Requires:      map[string]string{"@autono/beta": "latest"},
	}, "#!/bin/sh\necho alpha\n")
	beta := sourceBundle(t, "@autono/beta", button.Button{
		SchemaVersion: 1,
		Name:          "beta",
		Runtime:       "shell",
		Version:       "2.0.0",
	}, "#!/bin/sh\necho beta\n")
	src := memorySource{
		refs: []ButtonRef{
			{Name: "@autono/alpha", Kind: "button", Version: "1.0.0"},
			{Name: "@autono/beta", Kind: "button", Version: "2.0.0"},
		},
		bundles: map[string]*Bundle{
			"@autono/alpha@1.0.0": alpha,
			"@autono/beta@2.0.0":  beta,
		},
	}

	res, lock, err := InstallManifest(src, &manifest.Manifest{SchemaVersion: 1, Dependencies: map[string]string{"@autono/alpha": "latest"}}, nil, InstallOptions{RefreshFloating: true, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	got := append([]string{}, res.Installed...)
	sort.Strings(got)
	if len(got) != 2 || got[0] != "alpha" || got[1] != "beta" {
		t.Fatalf("want [alpha beta], got %v", res.Installed)
	}
	a := installedSpec(t, home, "alpha")
	if a.Version != "1.0.0" {
		t.Fatalf("alpha version = %q, want 1.0.0", a.Version)
	}
	data, err := os.ReadFile(filepath.Join(home, "buttons", "alpha", "button.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "source_name") || strings.Contains(string(data), "content_hash") {
		t.Fatalf("installed button.json should not contain source/content hash metadata: %s", data)
	}
	entry := lock.Dependencies["@autono/alpha"]
	if entry.Requested != "latest" || entry.Version != "1.0.0" || entry.ContentHash == "" || entry.InstalledName != "alpha" || entry.ResolvedAt != now.Format(time.RFC3339) {
		t.Fatalf("bad alpha lock entry: %+v", entry)
	}
	if dep := lock.Dependencies["@autono/beta"]; dep.Version != "2.0.0" || dep.Requested != "latest" {
		t.Fatalf("bad beta lock entry: %+v", dep)
	}
	info, err := os.Stat(filepath.Join(home, "buttons", "alpha", "main.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("main.sh not executable: %v", info.Mode())
	}
}

func TestInstallManifestHonorsExistingLock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)

	v1 := sourceBundle(t, "@autono/hello", button.Button{SchemaVersion: 1, Name: "hello", Runtime: "shell", Version: "1.0.0"}, "#!/bin/sh\necho v1\n")
	v2 := sourceBundle(t, "@autono/hello", button.Button{SchemaVersion: 1, Name: "hello", Runtime: "shell", Version: "1.1.0"}, "#!/bin/sh\necho v2\n")
	src := memorySource{
		refs: []ButtonRef{
			{Name: "@autono/hello", Kind: "button", Version: "1.0.0"},
			{Name: "@autono/hello", Kind: "button", Version: "1.1.0"},
		},
		bundles: map[string]*Bundle{
			"@autono/hello@1.0.0": v1,
			"@autono/hello@1.1.0": v2,
		},
	}
	m := &manifest.Manifest{SchemaVersion: 1, Dependencies: map[string]string{"@autono/hello": "latest"}}
	prior := &manifest.Lockfile{SchemaVersion: 1, Dependencies: map[string]manifest.LockEntry{
		"@autono/hello": {Kind: "button", Requested: "latest", Version: "1.0.0", ContentHash: "old", InstalledName: "hello", ResolvedAt: "then"},
	}}

	_, lock, err := InstallManifest(src, m, prior, InstallOptions{})
	if err != nil {
		t.Fatalf("install honoring lock: %v", err)
	}
	if got := lock.Dependencies["@autono/hello"].Version; got != "1.0.0" {
		t.Fatalf("install should honor locked version, got %s", got)
	}

	_, lock, err = InstallManifest(src, m, prior, InstallOptions{RefreshFloating: true})
	if err != nil {
		t.Fatalf("refresh install: %v", err)
	}
	if got := lock.Dependencies["@autono/hello"].Version; got != "1.1.0" {
		t.Fatalf("refresh should resolve latest, got %s", got)
	}
}

func TestResolveExactAndLatest(t *testing.T) {
	src := memorySource{refs: []ButtonRef{
		{Name: "@autono/hello", Kind: "button", Version: "1.0.0"},
		{Name: "@autono/hello", Kind: "button", Version: "1.1.0"},
	}}
	ref, err := Resolve(src, "@autono/hello", "")
	if err != nil || ref.Version != "1.1.0" {
		t.Fatalf("latest = %+v/%v, want 1.1.0", ref, err)
	}
	ref, err = Resolve(src, "@autono/hello", "1.0.0")
	if err != nil || ref.Version != "1.0.0" {
		t.Fatalf("exact = %+v/%v, want 1.0.0", ref, err)
	}
	if _, err := Resolve(src, "@autono/hello", "9.9.9"); err == nil {
		t.Fatal("missing exact version should error")
	}
}

type traversalSource struct{}

func (traversalSource) Index() ([]ButtonRef, error) {
	return []ButtonRef{{Name: "@autono/evil", Kind: "button", Version: "1.0.0"}}, nil
}
func (traversalSource) Fetch(name, version string) (*Bundle, error) {
	bj, _ := json.Marshal(button.Button{SchemaVersion: 1, Name: "evil", Runtime: "shell", Version: version})
	return &Bundle{
		Name:    name,
		Version: version,
		SHA256:  "deadbeef",
		Files:   map[string][]byte{"button.json": bj, "../escape.sh": []byte("x")},
	}, nil
}

func TestInstallRejectsTraversalBundle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	_, _, err := InstallManifest(traversalSource{}, &manifest.Manifest{SchemaVersion: 1, Dependencies: map[string]string{"@autono/evil": "1.0.0"}}, nil, InstallOptions{})
	if err == nil {
		t.Fatal("install should reject a bundle file that escapes the button dir")
	}
	if _, err := os.Stat(filepath.Join(home, "buttons", "evil")); !os.IsNotExist(err) {
		t.Fatal("rejected traversal install left a partial button dir behind")
	}
}

func writeSourceButton(t *testing.T, root string, b button.Button) {
	t.Helper()
	dir := filepath.Join(root, b.Name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.MarshalIndent(&b, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "button.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.sh"), []byte("#!/bin/sh\necho hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
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

func TestFetchSkipsSymlinks(t *testing.T) {
	src := t.TempDir()
	writeSourceButton(t, src, button.Button{SchemaVersion: 1, Name: "ok", Runtime: "shell", Version: "1.0.0"})
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("top secret"), 0o600); err != nil {
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
