package manifest

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManifestLoadMissing(t *testing.T) {
	_, err := LoadPath(filepath.Join(t.TempDir(), "buttons.json"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("LoadPath missing err = %v, want os.ErrNotExist", err)
	}
}

func TestManifestValidateDependencies(t *testing.T) {
	m := &Manifest{SchemaVersion: 1, Dependencies: map[string]string{
		"@autono/hello":  "latest",
		"@autono/deploy": "1.2.3",
	}}
	if err := m.Validate(); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}

	for _, tc := range []struct {
		name string
		deps map[string]string
	}{
		{"unscoped", map[string]string{"hello": "latest"}},
		{"empty-scope", map[string]string{"@/hello": "latest"}},
		{"nested", map[string]string{"@autono/a/b": "latest"}},
		{"range-not-mvp", map[string]string{"@autono/hello": "^1.2.0"}},
		{"bad-version", map[string]string{"@autono/hello": "v1.2.3"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &Manifest{SchemaVersion: 1, Dependencies: tc.deps}
			if err := m.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestManifestSaveLoadStable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "buttons.json")
	m := &Manifest{SchemaVersion: 1, Dependencies: map[string]string{
		"@autono/hello":  "latest",
		"@autono/deploy": "1.2.3",
	}}
	if err := SavePath(path, m); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := LoadPath(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Dependencies["@autono/hello"] != "latest" || got.Dependencies["@autono/deploy"] != "1.2.3" {
		t.Fatalf("loaded deps = %+v", got.Dependencies)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Fatal("manifest should end with newline")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("manifest mode = %o, want 600", info.Mode().Perm())
	}
}

func TestParsePackageSpec(t *testing.T) {
	for _, tc := range []struct {
		in        string
		name      string
		requested string
	}{
		{"@autono/hello", "@autono/hello", "latest"},
		{"@autono/hello@1.2.3", "@autono/hello", "1.2.3"},
		{"@autono/hello@1.2.3-beta.1", "@autono/hello", "1.2.3-beta.1"},
	} {
		t.Run(tc.in, func(t *testing.T) {
			name, requested, err := ParsePackageSpec(tc.in)
			if err != nil {
				t.Fatalf("ParsePackageSpec: %v", err)
			}
			if name != tc.name || requested != tc.requested {
				t.Fatalf("got %q/%q, want %q/%q", name, requested, tc.name, tc.requested)
			}
		})
	}
	for _, bad := range []string{"hello", "@autono/hello@latest", "@autono/hello@v1.2.3", "@autono"} {
		if _, _, err := ParsePackageSpec(bad); err == nil {
			t.Fatalf("ParsePackageSpec(%q) should fail", bad)
		}
	}
}
