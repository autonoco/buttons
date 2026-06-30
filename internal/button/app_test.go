package button

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/autonoco/buttons/internal/config"
)

func readApp(t *testing.T, home, name string) Button {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, "apps", name, "button.json"))
	if err != nil {
		t.Fatalf("read app button.json: %v", err)
	}
	var b Button
	if err := json.Unmarshal(data, &b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCreateAppEmptyScaffold(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)

	b, err := NewService().CreateApp(AppOpts{Name: "deckone", Description: "a deck"})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if b.Kind != "app" || b.Serve == nil || b.Serve.Type != "static" {
		t.Fatalf("unexpected button: %+v", b)
	}
	got := readApp(t, home, "deckone")
	if got.Kind != "app" || got.Serve == nil {
		t.Fatalf("on-disk button.json: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(home, "apps", "deckone", "AGENTS.md")); err != nil {
		t.Fatal("AGENTS.md missing")
	}
	// no main.* — apps are served, not pressed
	if _, err := os.Stat(filepath.Join(home, "apps", "deckone", "main.sh")); !os.IsNotExist(err) {
		t.Fatal("app must not scaffold main.sh")
	}
}

func TestCreateAppFromLocalPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "index.html"), []byte("<h1>hi</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := NewService().CreateApp(AppOpts{Name: "site", From: src}); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "apps", "site", "index.html")); err != nil {
		t.Fatal("source not copied into apps/")
	}
	readApp(t, home, "site") // button.json written alongside the copied source
}

func TestCreateAppFromGitURL(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	orig := gitClone
	defer func() { gitClone = orig }()

	var clonedURL string
	gitClone = func(url, dest string) error {
		clonedURL = url
		if err := os.MkdirAll(dest, 0o700); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dest, "README.md"), []byte("cloned"), 0o644)
	}

	if _, err := NewService().CreateApp(AppOpts{Name: "repo", From: "https://github.com/x/y"}); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if clonedURL != "https://github.com/x/y" {
		t.Fatalf("gitClone not invoked with the URL, got %q", clonedURL)
	}
	if _, err := os.Stat(filepath.Join(home, "apps", "repo", "README.md")); err != nil {
		t.Fatal("cloned content missing")
	}
}

func TestAppsNotScaffoldedByDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)

	if err := config.EnsureDataDir(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "apps")); !os.IsNotExist(err) {
		t.Fatal("apps/ must NOT exist until an app is created")
	}
	if _, err := NewService().CreateApp(AppOpts{Name: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "apps")); err != nil {
		t.Fatal("apps/ should exist after CreateApp")
	}
}

func TestCreateAppRejectsDuplicate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	if _, err := NewService().CreateApp(AppOpts{Name: "dup"}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewService().CreateApp(AppOpts{Name: "dup"}); err == nil {
		t.Fatal("duplicate app should error")
	}
}

func TestCreateAppRollsBackOnFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	orig := gitClone
	defer func() { gitClone = orig }()
	gitClone = func(url, dest string) error {
		_ = os.MkdirAll(dest, 0o700)
		_ = os.WriteFile(filepath.Join(dest, "partial"), []byte("x"), 0o644)
		return os.ErrInvalid // clone fails AFTER leaving a partial dir
	}
	if _, err := NewService().CreateApp(AppOpts{Name: "boom", From: "https://github.com/x/y"}); err == nil {
		t.Fatal("expected clone failure")
	}
	if _, err := os.Stat(filepath.Join(home, "apps", "boom")); !os.IsNotExist(err) {
		t.Fatal("partial app dir must be rolled back so a retry isn't blocked by ALREADY_EXISTS")
	}
}

func TestIsGitURL(t *testing.T) {
	for _, ok := range []string{"https://github.com/x/y", "http://x/y", "git@github.com:x/y.git", "x/y.git"} {
		if !isGitURL(ok) {
			t.Errorf("isGitURL(%q) = false, want true", ok)
		}
	}
	for _, no := range []string{"./local", "/abs/path", "../rel"} {
		if isGitURL(no) {
			t.Errorf("isGitURL(%q) = true, want false", no)
		}
	}
}
