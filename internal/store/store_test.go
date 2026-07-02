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
	"github.com/autonoco/buttons/internal/drawer"
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

func drawerBundle(t *testing.T, ref string, d drawer.Drawer) *Bundle {
	t.Helper()
	files := map[string][]byte{}
	data, err := json.MarshalIndent(&d, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	files["drawer.json"] = data
	return &Bundle{Name: ref, Kind: "drawer", Version: d.Version, SHA256: hashFiles(files), Files: files}
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

func installedDrawer(t *testing.T, home, name string) drawer.Drawer {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, "drawers", name, "drawer.json"))
	if err != nil {
		t.Fatalf("drawer %q not installed: %v", name, err)
	}
	var d drawer.Drawer
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestInstallManifestWithDepsWritesLock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

	alpha := sourceBundle(t, "@autono/alpha", button.Button{
		SchemaVersion: 1,
		Name:          "alpha",
		Runtime:       "shell",
		Version:       "1",
		Requires:      map[string]string{"@autono/beta": "latest"},
	}, "#!/bin/sh\necho alpha\n")
	beta := sourceBundle(t, "@autono/beta", button.Button{
		SchemaVersion: 1,
		Name:          "beta",
		Runtime:       "shell",
		Version:       "2",
	}, "#!/bin/sh\necho beta\n")
	src := memorySource{
		refs: []ButtonRef{
			{Name: "@autono/alpha", Kind: "button", Version: "1"},
			{Name: "@autono/beta", Kind: "button", Version: "2"},
		},
		bundles: map[string]*Bundle{
			"@autono/alpha@1": alpha,
			"@autono/beta@2":  beta,
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
	if a.Version != "1" {
		t.Fatalf("alpha version = %q, want 1", a.Version)
	}
	data, err := os.ReadFile(filepath.Join(home, "buttons", "alpha", "button.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "source_name") || strings.Contains(string(data), "content_hash") {
		t.Fatalf("installed button.json should not contain source/content hash metadata: %s", data)
	}
	entry := lock.Dependencies["@autono/alpha"]
	if entry.Requested != "latest" || entry.Version != "1" || entry.ContentHash == "" || entry.InstalledName != "alpha" || entry.ResolvedAt != now.Format(time.RFC3339) {
		t.Fatalf("bad alpha lock entry: %+v", entry)
	}
	if dep := lock.Dependencies["@autono/beta"]; dep.Version != "2" || dep.Requested != "latest" {
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

func TestInstallManifestInstallsDrawerPackageAndMemberButtons(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

	pack := drawerBundle(t, "@autono/deploy-pack", drawer.Drawer{
		SchemaVersion: drawer.SchemaVersion,
		Name:          "deploy-pack",
		Version:       "1",
		Steps: []drawer.Step{
			{ID: "build", Kind: "button", Button: "build"},
			{ID: "ship", Button: "ship"},
		},
	})
	pack.Files["helper.sh"] = []byte("#!/bin/sh\necho helper\n")
	pack.SHA256 = hashFiles(pack.Files)
	build := sourceBundle(t, "@autono/build", button.Button{
		SchemaVersion: 1,
		Name:          "build",
		Runtime:       "shell",
		Version:       "4",
	}, "#!/bin/sh\necho build\n")
	ship := sourceBundle(t, "@autono/ship", button.Button{
		SchemaVersion: 1,
		Name:          "ship",
		Runtime:       "shell",
		Version:       "2",
		Requires:      map[string]string{"@autono/base": "latest"},
	}, "#!/bin/sh\necho ship\n")
	base := sourceBundle(t, "@autono/base", button.Button{
		SchemaVersion: 1,
		Name:          "base",
		Runtime:       "shell",
		Version:       "3",
	}, "#!/bin/sh\necho base\n")
	src := memorySource{
		refs: []ButtonRef{
			{Name: "@autono/deploy-pack", Kind: "drawer", Version: "1"},
			{Name: "@autono/build", Kind: "button", Version: "4"},
			{Name: "@autono/ship", Kind: "button", Version: "2"},
			{Name: "@autono/base", Kind: "button", Version: "3"},
		},
		bundles: map[string]*Bundle{
			"@autono/deploy-pack@1": pack,
			"@autono/build@4":       build,
			"@autono/ship@2":        ship,
			"@autono/base@3":        base,
		},
	}

	res, lock, err := InstallManifest(src, &manifest.Manifest{SchemaVersion: 1, Dependencies: map[string]string{"@autono/deploy-pack": "latest"}}, nil, InstallOptions{RefreshFloating: true, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("install drawer package: %v", err)
	}
	got := append([]string{}, res.Installed...)
	sort.Strings(got)
	want := []string{"base", "build", "deploy-pack", "ship"}
	if len(got) != len(want) {
		t.Fatalf("installed = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("installed = %v, want %v", got, want)
		}
	}

	if d := installedDrawer(t, home, "deploy-pack"); d.Version != "1" || len(d.Steps) != 2 {
		t.Fatalf("installed drawer = %+v", d)
	}
	drawerInfo, err := os.Stat(filepath.Join(home, "drawers", "deploy-pack", "drawer.json"))
	if err != nil {
		t.Fatal(err)
	}
	if drawerInfo.Mode().Perm() != 0o600 {
		t.Fatalf("drawer.json mode = %v, want 0600", drawerInfo.Mode().Perm())
	}
	helperInfo, err := os.Stat(filepath.Join(home, "drawers", "deploy-pack", "helper.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if helperInfo.Mode().Perm() != 0o700 {
		t.Fatalf("helper.sh mode = %v, want 0700", helperInfo.Mode().Perm())
	}
	if installedSpec(t, home, "build").Version != "4" {
		t.Fatal("build button was not installed at version 4")
	}
	if installedSpec(t, home, "ship").Version != "2" {
		t.Fatal("ship button was not installed at version 2")
	}
	if installedSpec(t, home, "base").Version != "3" {
		t.Fatal("transitive base button was not installed at version 3")
	}

	for name, wantKind := range map[string]string{
		"@autono/deploy-pack": "drawer",
		"@autono/build":       "button",
		"@autono/ship":        "button",
		"@autono/base":        "button",
	} {
		entry := lock.Dependencies[name]
		if entry.Kind != wantKind || entry.Version == "" || entry.ContentHash == "" || entry.ResolvedAt != now.Format(time.RFC3339) {
			t.Fatalf("bad lock entry for %s: %+v", name, entry)
		}
	}
}

func TestInstallManifestHonorsExistingLock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)

	v1 := sourceBundle(t, "@autono/hello", button.Button{SchemaVersion: 1, Name: "hello", Runtime: "shell", Version: "1"}, "#!/bin/sh\necho v1\n")
	v2 := sourceBundle(t, "@autono/hello", button.Button{SchemaVersion: 1, Name: "hello", Runtime: "shell", Version: "2"}, "#!/bin/sh\necho v2\n")
	src := memorySource{
		refs: []ButtonRef{
			{Name: "@autono/hello", Kind: "button", Version: "1"},
			{Name: "@autono/hello", Kind: "button", Version: "2"},
		},
		bundles: map[string]*Bundle{
			"@autono/hello@1": v1,
			"@autono/hello@2": v2,
		},
	}
	m := &manifest.Manifest{SchemaVersion: 1, Dependencies: map[string]string{"@autono/hello": "latest"}}
	prior := &manifest.Lockfile{SchemaVersion: 1, Dependencies: map[string]manifest.LockEntry{
		"@autono/hello": {Kind: "button", Requested: "latest", Version: "1", ContentHash: "old", InstalledName: "hello", ResolvedAt: "then"},
	}}

	_, lock, err := InstallManifest(src, m, prior, InstallOptions{})
	if err != nil {
		t.Fatalf("install honoring lock: %v", err)
	}
	if got := lock.Dependencies["@autono/hello"].Version; got != "1" {
		t.Fatalf("install should honor locked version, got %s", got)
	}

	_, lock, err = InstallManifest(src, m, prior, InstallOptions{RefreshFloating: true})
	if err != nil {
		t.Fatalf("refresh install: %v", err)
	}
	if got := lock.Dependencies["@autono/hello"].Version; got != "2" {
		t.Fatalf("refresh should resolve latest, got %s", got)
	}
}

func TestResolveExactAndLatest(t *testing.T) {
	src := memorySource{refs: []ButtonRef{
		{Name: "@autono/hello", Kind: "button", Version: "1"},
		{Name: "@autono/hello", Kind: "button", Version: "2"},
	}}
	ref, err := Resolve(src, "@autono/hello", "")
	if err != nil || ref.Version != "2" {
		t.Fatalf("latest = %+v/%v, want 2", ref, err)
	}
	ref, err = Resolve(src, "@autono/hello", "1")
	if err != nil || ref.Version != "1" {
		t.Fatalf("exact = %+v/%v, want 1", ref, err)
	}
	if _, err := Resolve(src, "@autono/hello", "9.9.9"); err == nil {
		t.Fatal("missing exact version should error")
	}
}

type traversalSource struct{}

func (traversalSource) Index() ([]ButtonRef, error) {
	return []ButtonRef{{Name: "@autono/evil", Kind: "button", Version: "1"}}, nil
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
	_, _, err := InstallManifest(traversalSource{}, &manifest.Manifest{SchemaVersion: 1, Dependencies: map[string]string{"@autono/evil": "1"}}, nil, InstallOptions{})
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
	writeSourceButton(t, src, button.Button{SchemaVersion: 1, Name: "ok", Runtime: "shell", Version: "1"})
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
	writeSourceButton(t, src, button.Button{SchemaVersion: 1, Name: "pin", Runtime: "shell", Version: "1"})
	ls := &LocalSource{Root: src}
	if _, err := ls.Fetch("pin", "9.9.9"); err == nil {
		t.Error("Fetch with mismatched version pin should error")
	}
	if _, err := ls.Fetch("pin", "1"); err != nil {
		t.Errorf("Fetch with matching version pin should succeed: %v", err)
	}
	if _, err := ls.Fetch("pin", ""); err != nil {
		t.Errorf("Fetch with empty version (latest) should succeed: %v", err)
	}
}

func TestIndexRejectsAmbiguousPackageSpec(t *testing.T) {
	src := t.TempDir()
	writeSourceButton(t, src, button.Button{SchemaVersion: 1, Name: "ambiguous", Runtime: "shell", Version: "1"})
	data, _ := json.MarshalIndent(&drawer.Drawer{SchemaVersion: drawer.SchemaVersion, Name: "ambiguous", Version: "1"}, "", "  ")
	if err := os.WriteFile(filepath.Join(src, "ambiguous", "drawer.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := (&LocalSource{Root: src}).Index()
	if err == nil || !strings.Contains(err.Error(), "ambiguous package") {
		t.Fatalf("Index error = %v, want ambiguous package", err)
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
	writeSourceButton(t, src, button.Button{SchemaVersion: 1, Name: "ok", Runtime: "shell", Version: "1"})
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
