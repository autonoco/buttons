package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestButtonDir_Safe verifies that valid, already-sanitized names
// resolve to a path inside the buttons data directory.
func TestButtonDir_Safe(t *testing.T) {
	t.Setenv("BUTTONS_HOME", t.TempDir())

	safeNames := []string{
		"hello",
		"my-button",
		"abc123",
		"a",
		"valid-name-with-many-hyphens",
	}

	buttonsDir, err := ButtonsDir()
	if err != nil {
		t.Fatalf("ButtonsDir() error = %v", err)
	}

	for _, name := range safeNames {
		t.Run(name, func(t *testing.T) {
			got, err := ButtonDir(name)
			if err != nil {
				t.Fatalf("ButtonDir(%q) unexpected error: %v", name, err)
			}
			want := filepath.Join(buttonsDir, name)
			if got != want {
				t.Errorf("ButtonDir(%q) = %q, want %q", name, got, want)
			}
			if !strings.HasPrefix(got, buttonsDir+string(filepath.Separator)) {
				t.Errorf("ButtonDir(%q) escaped buttons dir: %q", name, got)
			}
		})
	}
}

// TestButtonDir_Traversal verifies defense-in-depth — if a raw
// user-controlled name somehow reaches ButtonDir without going through
// Slugify/ValidateName, any path that would escape the data directory
// must be rejected.
func TestButtonDir_Traversal(t *testing.T) {
	t.Setenv("BUTTONS_HOME", t.TempDir())

	traversalNames := []string{
		"../evil",
		"../../evil",
		"../../../../../etc/passwd",
		"foo/../../../bar",
	}

	for _, name := range traversalNames {
		t.Run(name, func(t *testing.T) {
			_, err := ButtonDir(name)
			if err == nil {
				t.Errorf("ButtonDir(%q) should have rejected traversal attempt", name)
			}
		})
	}
}

// TestButtonDir_EmptyAndRoot verifies that an empty name or a name that
// resolves exactly to the data directory is rejected — there must be a
// concrete child component.
func TestButtonDir_EmptyAndRoot(t *testing.T) {
	t.Setenv("BUTTONS_HOME", t.TempDir())

	edgeCases := []string{
		"",
		".",
	}

	for _, name := range edgeCases {
		t.Run(name, func(t *testing.T) {
			_, err := ButtonDir(name)
			if err == nil {
				t.Errorf("ButtonDir(%q) should have been rejected", name)
			}
		})
	}
}
