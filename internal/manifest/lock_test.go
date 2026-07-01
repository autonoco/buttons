package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLockfileSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "buttons-lock.json")
	lock := &Lockfile{SchemaVersion: 1, Dependencies: map[string]LockEntry{
		"@autono/hello": {
			Kind:          "button",
			Requested:     "latest",
			Version:       "1",
			ContentHash:   "sha256:abc",
			InstalledName: "hello",
			ResolvedAt:    "2026-07-01T12:00:00Z",
		},
	}}
	if err := SaveLockfilePath(path, lock); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := LoadLockfilePath(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	entry := got.Dependencies["@autono/hello"]
	if entry.Version != "1" || entry.Requested != "latest" || entry.InstalledName != "hello" {
		t.Fatalf("loaded entry = %+v", entry)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("lock mode = %o, want 600", info.Mode().Perm())
	}
}

func TestLockfileValidation(t *testing.T) {
	valid := LockEntry{
		Kind:          "button",
		Requested:     "latest",
		Version:       "1",
		ContentHash:   "sha256:abc",
		InstalledName: "hello",
		ResolvedAt:    "2026-07-01T12:00:00Z",
	}
	for _, tc := range []struct {
		name  string
		entry LockEntry
	}{
		{"bad-kind", func() LockEntry { e := valid; e.Kind = "tag"; return e }()},
		{"bad-version", func() LockEntry { e := valid; e.Version = "latest"; return e }()},
		{"empty-hash", func() LockEntry { e := valid; e.ContentHash = ""; return e }()},
		{"empty-installed-name", func() LockEntry { e := valid; e.InstalledName = ""; return e }()},
		{"empty-resolved-at", func() LockEntry { e := valid; e.ResolvedAt = ""; return e }()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lock := &Lockfile{SchemaVersion: 1, Dependencies: map[string]LockEntry{"@autono/hello": tc.entry}}
			if err := lock.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
