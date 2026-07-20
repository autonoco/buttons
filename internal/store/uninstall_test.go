package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/drawer"
	"github.com/autonoco/buttons/internal/manifest"
)

func assertStrings(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s = %v, want %v", label, got, want)
		}
	}
}

func TestUninstall(t *testing.T) {
	cases := []struct {
		name          string
		remove        string
		alphaRequires map[string]string
		tamper        func(t *testing.T, home string)
		wantRemoved   []string
		wantKept      []string
		wantLockEntry bool // lock still has the removed package afterwards
		wantDir       bool // the package dir still exists afterwards
	}{
		{
			name:        "removes package dir and lock entry",
			remove:      "@autono/alpha",
			wantRemoved: []string{"alpha"},
		},
		{
			name:          "keeps package another locked package still requires",
			remove:        "@autono/beta",
			alphaRequires: map[string]string{"@autono/beta": "latest"},
			wantKept:      []string{"beta"},
			wantLockEntry: true,
			wantDir:       true,
		},
		{
			name:   "never deletes a dir without install state",
			remove: "@autono/alpha",
			tamper: func(t *testing.T, home string) {
				t.Helper()
				if err := os.Remove(filepath.Join(home, "buttons", "alpha", InstallStateFile)); err != nil {
					t.Fatal(err)
				}
			},
			wantKept: []string{"alpha"},
			wantDir:  true,
		},
		{
			name:   "no-op for a package that was never installed",
			remove: "@autono/ghost",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("BUTTONS_HOME", home)

			alpha := sourceBundle(t, "@autono/alpha", button.Button{
				SchemaVersion: 1,
				Name:          "alpha",
				Runtime:       "shell",
				Version:       "1",
				Requires:      tc.alphaRequires,
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
			m := &manifest.Manifest{SchemaVersion: 1, Dependencies: map[string]string{
				"@autono/alpha": "latest",
				"@autono/beta":  "latest",
			}}
			_, lock, err := InstallManifest(src, m, nil, InstallOptions{RefreshFloating: true})
			if err != nil {
				t.Fatalf("install: %v", err)
			}
			if tc.tamper != nil {
				tc.tamper(t, home)
			}

			res, err := Uninstall(lock, tc.remove)
			if err != nil {
				t.Fatalf("Uninstall(%q): %v", tc.remove, err)
			}
			assertStrings(t, "removed", res.Removed, tc.wantRemoved)
			assertStrings(t, "kept", res.Kept, tc.wantKept)

			if _, ok := lock.Dependencies[tc.remove]; ok != tc.wantLockEntry {
				t.Fatalf("lock entry present = %v, want %v", ok, tc.wantLockEntry)
			}
			local := tc.remove[strings.LastIndex(tc.remove, "/")+1:]
			dir := filepath.Join(home, "buttons", local)
			if _, err := os.Stat(dir); (err == nil) != tc.wantDir {
				t.Fatalf("dir exists = %v (stat err %v), want %v", err == nil, err, tc.wantDir)
			}
			if tc.wantDir {
				for _, f := range []string{"button.json", "main.sh"} {
					if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
						t.Fatalf("kept package file %s missing: %v", f, err)
					}
				}
			}

			// Every package not named in the call is untouched.
			for pkg, localName := range map[string]string{"@autono/alpha": "alpha", "@autono/beta": "beta"} {
				if pkg == tc.remove {
					continue
				}
				if _, ok := lock.Dependencies[pkg]; !ok {
					t.Fatalf("lock lost %s", pkg)
				}
				otherDir := filepath.Join(home, "buttons", localName)
				for _, f := range []string{"button.json", "main.sh", InstallStateFile} {
					if _, err := os.Stat(filepath.Join(otherDir, f)); err != nil {
						t.Fatalf("%s file %s missing: %v", pkg, f, err)
					}
				}
			}
		})
	}
}

func TestUninstallDrawerPackageKeepsMemberButtons(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)

	pack := drawerBundle(t, "@autono/deploy-pack", drawer.Drawer{
		SchemaVersion: drawer.SchemaVersion,
		Name:          "deploy-pack",
		Version:       "1",
		Steps:         []drawer.Step{{ID: "build", Kind: "button", Button: "build"}},
	})
	build := sourceBundle(t, "@autono/build", button.Button{
		SchemaVersion: 1,
		Name:          "build",
		Runtime:       "shell",
		Version:       "1",
	}, "#!/bin/sh\necho build\n")
	src := memorySource{
		refs: []ButtonRef{
			{Name: "@autono/deploy-pack", Kind: "drawer", Version: "1"},
			{Name: "@autono/build", Kind: "button", Version: "1"},
		},
		bundles: map[string]*Bundle{
			"@autono/deploy-pack@1": pack,
			"@autono/build@1":       build,
		},
	}
	m := &manifest.Manifest{SchemaVersion: 1, Dependencies: map[string]string{"@autono/deploy-pack": "latest"}}
	_, lock, err := InstallManifest(src, m, nil, InstallOptions{RefreshFloating: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}

	res, err := Uninstall(lock, "@autono/deploy-pack")
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	assertStrings(t, "removed", res.Removed, []string{"deploy-pack"})
	if _, ok := lock.Dependencies["@autono/deploy-pack"]; ok {
		t.Fatal("lock still lists @autono/deploy-pack")
	}
	if _, err := os.Stat(filepath.Join(home, "drawers", "deploy-pack")); !os.IsNotExist(err) {
		t.Fatalf("drawer dir should be gone, stat err = %v", err)
	}
	// Member buttons are transitive deps; uninstall does not cascade — the
	// next full install rebuilds the lock from the manifest and drops orphans.
	if _, err := os.Stat(filepath.Join(home, "buttons", "build", "button.json")); err != nil {
		t.Fatalf("member button should remain installed: %v", err)
	}
	if _, ok := lock.Dependencies["@autono/build"]; !ok {
		t.Fatal("member button lock entry should remain")
	}
}
