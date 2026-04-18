package history

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestPruneOldRuns_KeepsMostRecent(t *testing.T) {
	dir := t.TempDir()
	// Filenames are ISO-ish UTC timestamps in Record — use strings that
	// sort correctly for this test.
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("2026-04-18T00-00-%02d.json", i)
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := pruneOldRuns(dir, 3); err != nil {
		t.Fatalf("prune: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 3 {
		t.Fatalf("entries after prune = %d, want 3", len(entries))
	}

	// Remaining should be the three most recent names (sorted ascending,
	// the last three are the newest by our timestamp convention).
	wantRemaining := map[string]bool{
		"2026-04-18T00-00-07.json": true,
		"2026-04-18T00-00-08.json": true,
		"2026-04-18T00-00-09.json": true,
	}
	for _, e := range entries {
		if !wantRemaining[e.Name()] {
			t.Errorf("unexpected survivor: %s", e.Name())
		}
	}
}

func TestPruneOldRuns_UnderLimit_NoOp(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("2026-04-18T00-00-%02d.json", i)
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := pruneOldRuns(dir, 10); err != nil {
		t.Fatalf("prune: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 5 {
		t.Errorf("entries = %d, want 5 (all survive)", len(entries))
	}
}

func TestPruneOldRuns_IgnoresNonJSON(t *testing.T) {
	dir := t.TempDir()
	// Mix json, non-json, and a subdir. Only json files are counted /
	// subject to deletion.
	if err := os.WriteFile(filepath.Join(dir, "a.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "c.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.txt"), []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := pruneOldRuns(dir, 2); err != nil {
		t.Fatal(err)
	}

	// Expect: README.txt + sub/ untouched; c.json, b.json survive; a.json gone.
	for _, name := range []string{"README.txt", "sub", "b.json", "c.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s should still exist: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "a.json")); !os.IsNotExist(err) {
		t.Errorf("a.json should be deleted; err = %v", err)
	}
}
