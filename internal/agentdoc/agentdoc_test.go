package agentdoc

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestPath_PrefersCanonical(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, Name))
	write(t, filepath.Join(dir, Legacy))
	if got, want := Path(dir), filepath.Join(dir, Name); got != want {
		t.Errorf("both present: want canonical %q, got %q", want, got)
	}
}

func TestPath_FallsBackToLegacy(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, Legacy)) // only the pre-rename file exists
	if got, want := Path(dir), filepath.Join(dir, Legacy); got != want {
		t.Errorf("legacy only: want %q, got %q", want, got)
	}
}

func TestPath_DefaultsToCanonicalWhenMissing(t *testing.T) {
	dir := t.TempDir()
	if got, want := Path(dir), filepath.Join(dir, Name); got != want {
		t.Errorf("neither present: want canonical %q, got %q", want, got)
	}
}
