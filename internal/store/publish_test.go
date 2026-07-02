package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/drawer"
)

type capturePublisher struct {
	bundle *Bundle
}

func (p *capturePublisher) Publish(b *Bundle) error {
	p.bundle = b
	return nil
}

type duplicateOncePublisher struct {
	versions []string
}

func (p *duplicateOncePublisher) Publish(b *Bundle) error {
	p.versions = append(p.versions, b.Version)
	if len(p.versions) == 1 {
		return &registryResponseError{statusCode: 409, code: "VERSION_EXISTS", message: "versions are immutable", what: "publish"}
	}
	return nil
}

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

func writeInstalledDrawer(t *testing.T, home string, d drawer.Drawer) {
	t.Helper()
	dir := filepath.Join(home, "drawers", d.Name)
	if err := os.MkdirAll(filepath.Join(dir, "pressed"), 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.MarshalIndent(&d, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "drawer.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# "+d.Name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
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

func TestPublishToRegistryStampsAutomaticVersion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	writeInstalledButton(t, home, button.Button{SchemaVersion: 1, Name: "hello", Runtime: "shell"}, "#!/bin/sh\necho hello\n")

	pub := &capturePublisher{}
	res, err := PublishToRegistry(pub, "@autono/hello")
	if err != nil {
		t.Fatalf("publish to registry: %v", err)
	}
	if res.Name != "@autono/hello" {
		t.Fatalf("name = %q, want @autono/hello", res.Name)
	}
	if res.Version != "1" {
		t.Fatalf("version = %q, want 1", res.Version)
	}
	if pub.bundle == nil {
		t.Fatal("publisher did not receive bundle")
	}
	if pub.bundle.Version != res.Version {
		t.Fatalf("bundle version = %q, want %q", pub.bundle.Version, res.Version)
	}

	var bundled button.Button
	if err := json.Unmarshal(pub.bundle.Files["button.json"], &bundled); err != nil {
		t.Fatalf("bundled button.json: %v", err)
	}
	if bundled.Version != res.Version {
		t.Fatalf("bundled button.json version = %q, want %q", bundled.Version, res.Version)
	}

	data, err := os.ReadFile(filepath.Join(home, "buttons", "hello", "button.json"))
	if err != nil {
		t.Fatalf("read local button.json: %v", err)
	}
	var local button.Button
	if err := json.Unmarshal(data, &local); err != nil {
		t.Fatalf("local button.json: %v", err)
	}
	if local.Version != res.Version {
		t.Fatalf("local button.json version = %q, want %q", local.Version, res.Version)
	}
}

func TestPublishToRegistryAutoDetectsDrawerPackage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	writeInstalledDrawer(t, home, drawer.Drawer{
		SchemaVersion: drawer.SchemaVersion,
		Name:          "deploy-pack",
		Steps:         []drawer.Step{{ID: "build", Button: "build"}},
	})

	pub := &capturePublisher{}
	res, err := PublishToRegistry(pub, "@autono/deploy-pack")
	if err != nil {
		t.Fatalf("publish drawer to registry: %v", err)
	}
	if res.Name != "@autono/deploy-pack" || res.Kind != "drawer" || res.Version != "1" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if pub.bundle == nil || pub.bundle.Kind != "drawer" {
		t.Fatalf("publisher bundle = %+v, want drawer kind", pub.bundle)
	}
	if _, ok := pub.bundle.Files["drawer.json"]; !ok {
		t.Fatal("published drawer bundle missing drawer.json")
	}
	if _, ok := pub.bundle.Files["pressed/run1.json"]; ok {
		t.Fatal("drawer pressed history must not be published")
	}

	data, err := os.ReadFile(filepath.Join(home, "drawers", "deploy-pack", "drawer.json"))
	if err != nil {
		t.Fatalf("read local drawer.json: %v", err)
	}
	var local drawer.Drawer
	if err := json.Unmarshal(data, &local); err != nil {
		t.Fatalf("local drawer.json: %v", err)
	}
	if local.Version != "1" {
		t.Fatalf("local drawer version = %q, want 1", local.Version)
	}
}

func TestPublishToRegistryRejectsLocalButtonDrawerCollision(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	writeInstalledButton(t, home, button.Button{SchemaVersion: 1, Name: "deploy", Runtime: "shell", Version: "1"}, "#!/bin/sh\necho deploy\n")
	writeInstalledDrawer(t, home, drawer.Drawer{SchemaVersion: drawer.SchemaVersion, Name: "deploy", Version: "1"})

	_, err := PublishToRegistry(&capturePublisher{}, "@autono/deploy")
	if err == nil {
		t.Fatal("publishing a colliding button/drawer name should error")
	}
	if !strings.Contains(err.Error(), "found both button and drawer") {
		t.Fatalf("error = %v, want collision message", err)
	}
}

func TestPublishToRegistryBumpsSimpleVersionOnConflict(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	writeInstalledButton(t, home, button.Button{SchemaVersion: 1, Name: "hello", Runtime: "shell", Version: "1"}, "#!/bin/sh\necho hello\n")

	pub := &duplicateOncePublisher{}
	res, err := PublishToRegistry(pub, "@autono/hello")
	if err != nil {
		t.Fatalf("publish to registry: %v", err)
	}
	if res.Version != "2" {
		t.Fatalf("version = %q, want 2", res.Version)
	}
	if got, want := pub.versions, []string{"1", "2"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("published versions = %v, want %v", got, want)
	}

	data, err := os.ReadFile(filepath.Join(home, "buttons", "hello", "button.json"))
	if err != nil {
		t.Fatalf("read local button.json: %v", err)
	}
	var local button.Button
	if err := json.Unmarshal(data, &local); err != nil {
		t.Fatalf("local button.json: %v", err)
	}
	if local.Version != "2" {
		t.Fatalf("local button.json version = %q, want 2", local.Version)
	}
}
