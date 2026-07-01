package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/autonoco/buttons/internal/button"
)

// writeInstalledButton drops a local button into BUTTONS_HOME (as if created).
func writeInstalledButton(t *testing.T, home string, b button.Button, body string) {
	t.Helper()
	dir := filepath.Join(home, "buttons", b.Name)
	if err := os.MkdirAll(filepath.Join(dir, "pressed"), 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.MarshalIndent(&b, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "button.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.sh"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	// A run-history file that must NOT be published.
	if err := os.WriteFile(filepath.Join(dir, "pressed", "run1.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPublishThenInstallRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	writeInstalledButton(t, home, button.Button{SchemaVersion: 1, Name: "deploy", Runtime: "shell", Version: "1.2.0", Tags: []string{"ops"}}, "#!/bin/sh\necho deploy\n")

	pack := t.TempDir()
	dst := &LocalSource{Root: pack}

	res, err := Publish(dst, "deploy")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if res.Name != "deploy" || res.Version != "1.2.0" || res.SHA256 == "" {
		t.Fatalf("unexpected publish result: %+v", res)
	}
	// pressed/ history is not part of the artifact (button.json + main.sh only).
	if res.Files != 2 {
		t.Fatalf("expected 2 published files (button.json, main.sh), got %d", res.Files)
	}
	if _, err := os.Stat(filepath.Join(pack, "deploy", "pressed")); !os.IsNotExist(err) {
		t.Fatal("pressed/ history must not be published")
	}

	// The published button is now fetchable from the local source.
	fetched, err := dst.Fetch("deploy", "1.2.0")
	if err != nil {
		t.Fatalf("fetch of published button: %v", err)
	}
	if fetched.Version != "1.2.0" || fetched.SHA256 != res.SHA256 {
		t.Fatalf("round-trip lost metadata: version=%q hash=%q publish_hash=%q", fetched.Version, fetched.SHA256, res.SHA256)
	}
}

func TestPublishMissingButton(t *testing.T) {
	t.Setenv("BUTTONS_HOME", t.TempDir())
	if _, err := Publish(&LocalSource{Root: t.TempDir()}, "ghost"); err == nil {
		t.Fatal("publishing a missing button should error")
	}
}
