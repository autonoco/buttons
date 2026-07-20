package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autonoco/buttons/internal/manifest"
	"github.com/autonoco/buttons/internal/store"
)

func assertStringSlice(t *testing.T, label string, got, want []string) {
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

func TestRunRemove(t *testing.T) {
	cases := []struct {
		name          string
		remove        string
		helloRequires map[string]string // requires stamped on @autono/hello
		wantErr       string
		wantRemoved   []string
		wantKept      []string
	}{
		{
			name:        "removes dep, its dir, and lock entry",
			remove:      "@autono/hello",
			wantRemoved: []string{"hello"},
		},
		{
			name:          "keeps package another dep still requires",
			remove:        "@autono/other",
			helloRequires: map[string]string{"@autono/other": "latest"},
			wantKept:      []string{"other"},
		},
		{
			name:    "errors for a package not in the manifest",
			remove:  "@autono/ghost",
			wantErr: "not a dependency",
		},
		{
			name:    "suggests delete for unscoped local names",
			remove:  "hello",
			wantErr: "buttons delete hello",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("BUTTONS_HOME", home)
			reg := newTestRegistry(t)
			reg.publishButton(t, "@autono/hello", "1", tc.helloRequires)
			reg.publishButton(t, "@autono/other", "1", nil)
			for _, spec := range []string{"@autono/hello", "@autono/other"} {
				if _, _, _, err := runAdd(context.Background(), spec, true); err != nil {
					t.Fatalf("add %s: %v", spec, err)
				}
			}

			res, name, err := runRemove(context.Background(), tc.remove)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("runRemove(%q) error = %v, want it to contain %q", tc.remove, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("runRemove(%q): %v", tc.remove, err)
			}
			if name != tc.remove {
				t.Fatalf("name = %q, want %q", name, tc.remove)
			}
			assertStringSlice(t, "removed", res.Removed, tc.wantRemoved)
			assertStringSlice(t, "kept", res.Kept, tc.wantKept)

			m, err := manifest.Load()
			if err != nil {
				t.Fatalf("load manifest: %v", err)
			}
			if _, ok := m.Dependencies[tc.remove]; ok {
				t.Fatalf("manifest still lists %s", tc.remove)
			}
			lock, err := manifest.LoadLockfile()
			if err != nil {
				t.Fatalf("load lockfile: %v", err)
			}
			local := tc.remove[strings.LastIndex(tc.remove, "/")+1:]
			removedDir := filepath.Join(home, "buttons", local)
			if len(tc.wantRemoved) > 0 {
				if _, ok := lock.Dependencies[tc.remove]; ok {
					t.Fatalf("lock still lists %s", tc.remove)
				}
				if _, err := os.Stat(removedDir); !os.IsNotExist(err) {
					t.Fatalf("dir %s should be gone, stat err = %v", removedDir, err)
				}
			}
			if len(tc.wantKept) > 0 {
				if _, ok := lock.Dependencies[tc.remove]; !ok {
					t.Fatalf("lock entry for still-required %s should remain", tc.remove)
				}
				if _, err := os.Stat(filepath.Join(removedDir, "button.json")); err != nil {
					t.Fatalf("still-required package files should remain: %v", err)
				}
			}

			// The other dependency is untouched.
			other := "@autono/other"
			if tc.remove == other {
				other = "@autono/hello"
			}
			if _, ok := m.Dependencies[other]; !ok {
				t.Fatalf("manifest lost %s", other)
			}
			if _, ok := lock.Dependencies[other]; !ok {
				t.Fatalf("lock lost %s", other)
			}
			otherDir := filepath.Join(home, "buttons", other[strings.LastIndex(other, "/")+1:])
			for _, f := range []string{"button.json", "main.sh", store.InstallStateFile} {
				if _, err := os.Stat(filepath.Join(otherDir, f)); err != nil {
					t.Fatalf("other dep file %s missing: %v", f, err)
				}
			}
		})
	}
}
