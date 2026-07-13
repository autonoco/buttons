package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/drawer"
	"github.com/autonoco/buttons/internal/manifest"
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
	if err := os.MkdirAll(filepath.Join(dir, "pressed"), 0o700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.MarshalIndent(&d, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "drawer.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# "+d.Name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pressed", "run1.json"), []byte("{}"), 0o600); err != nil {
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

func TestPublishToRegistryAddsNormalizedFlowDefinition(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	flow := &drawer.FlowDefinition{
		InitialStage: "intake",
		Manager:      drawer.FlowManager{Agent: "activation.manager", HeartbeatSeconds: 300},
		Stages: []drawer.FlowStage{
			{ID: "intake", Title: "Intake", Transitions: []string{"done"}},
			{ID: "done", Title: "Done"},
		},
	}
	writeInstalledDrawer(t, home, drawer.Drawer{
		SchemaVersion: drawer.SchemaVersion,
		Name:          "software-delivery",
		DrawerKind:    drawer.DrawerKindFlow,
		Description:   "Move a ticket through delivery.",
		Version:       "1",
		Flow:          flow,
	})

	pub := &capturePublisher{}
	res, err := PublishToRegistry(pub, "@autono/software-delivery")
	if err != nil {
		t.Fatalf("publish flow drawer: %v", err)
	}
	if pub.bundle == nil {
		t.Fatal("publisher did not receive bundle")
	}
	normalized, ok := pub.bundle.Files["flow-definition.json"]
	if !ok {
		t.Fatalf("bundle files = %v; want flow-definition.json", pub.bundle.Files)
	}
	if string(pub.bundle.FlowDefinition) != string(normalized) {
		t.Fatalf("bundle FlowDefinition does not match published normalized file")
	}
	if pub.bundle.FlowDefinitionSHA256 == "" {
		t.Fatal("flow definition hash is empty")
	}
	if res.FlowDefinitionSHA256 != pub.bundle.FlowDefinitionSHA256 {
		t.Fatalf("publish result flow hash = %q, want %q", res.FlowDefinitionSHA256, pub.bundle.FlowDefinitionSHA256)
	}

	var got map[string]any
	if err := json.Unmarshal(normalized, &got); err != nil {
		t.Fatalf("normalized definition is invalid JSON: %v", err)
	}
	if got["schema_version"] != float64(2) || got["name"] != "software-delivery" || got["drawer_kind"] != "flow" || got["version"] != "1" {
		t.Fatalf("normalized identity = %#v", got)
	}
	for _, excluded := range []string{"steps", "created_at", "updated_at"} {
		if _, exists := got[excluded]; exists {
			t.Errorf("normalized definition must exclude %s", excluded)
		}
	}
	gotFlow, err := json.Marshal(got["flow"])
	if err != nil {
		t.Fatal(err)
	}
	wantFlow, err := json.Marshal(flow)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotFlow) != string(wantFlow) {
		t.Fatalf("flow changed during normalization:\n got %s\nwant %s", gotFlow, wantFlow)
	}
}

func TestFlowDrawerPublishInstallRoundTripPreservesDefinition(t *testing.T) {
	authorHome := t.TempDir()
	t.Setenv("BUTTONS_HOME", authorHome)
	svc := drawer.NewService()
	if _, err := svc.CreateWithKind("software-delivery", "Move a ticket through delivery.", nil, drawer.DrawerKindFlow); err != nil {
		t.Fatal(err)
	}
	for _, stage := range []struct{ id, title string }{{"intake", "Intake"}, {"done", "Done"}} {
		if _, err := svc.AddFlowStage("software-delivery", stage.id, stage.title, ""); err != nil {
			t.Fatal(err)
		}
	}
	for path, value := range map[string]any{
		"initial_stage":                             "intake",
		"manager.agent":                             "activation.manager",
		"manager.heartbeat_seconds":                 300,
		"stages.intake.transitions":                 []string{"done"},
		"stages.intake.worker.agent":                "activation.worker",
		"stages.intake.completion.requires_summary": true,
	} {
		if _, err := svc.SetFlowField("software-delivery", path, value); err != nil {
			t.Fatalf("set flow.%s: %v", path, err)
		}
	}
	want, err := svc.Get("software-delivery")
	if err != nil {
		t.Fatal(err)
	}

	pub := &capturePublisher{}
	if _, err := PublishToRegistry(pub, "@autono/software-delivery"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	installHome := t.TempDir()
	t.Setenv("BUTTONS_HOME", installHome)
	src := memorySource{
		refs: []ButtonRef{{Name: "@autono/software-delivery", Kind: "drawer", Version: pub.bundle.Version}},
		bundles: map[string]*Bundle{
			"@autono/software-delivery@" + pub.bundle.Version: pub.bundle,
		},
	}
	if _, _, err := InstallManifest(src, &manifest.Manifest{
		SchemaVersion: 1,
		Dependencies:  map[string]string{"@autono/software-delivery": pub.bundle.Version},
	}, nil, InstallOptions{}); err != nil {
		t.Fatalf("install: %v", err)
	}
	got, err := drawer.NewService().Get("software-delivery")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Flow, want.Flow) {
		t.Fatalf("flow definition changed:\n got %#v\nwant %#v", got.Flow, want.Flow)
	}
	if _, err := os.Stat(filepath.Join(installHome, "drawers", "software-delivery", "flow-definition.json")); err != nil {
		t.Fatalf("installed normalized definition: %v", err)
	}
}

func TestPublishToRegistrySkipsDrawerSymlinks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	writeInstalledDrawer(t, home, drawer.Drawer{
		SchemaVersion: drawer.SchemaVersion,
		Name:          "deploy-pack",
		Steps:         []drawer.Step{{ID: "build", Button: "build"}},
	})
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("top secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(home, "drawers", "deploy-pack", "leak.txt")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	pub := &capturePublisher{}
	if _, err := PublishToRegistry(pub, "@autono/deploy-pack"); err != nil {
		t.Fatalf("publish drawer to registry: %v", err)
	}
	if pub.bundle == nil {
		t.Fatal("publisher did not receive bundle")
	}
	if _, leaked := pub.bundle.Files["leak.txt"]; leaked {
		t.Fatal("drawer publish followed a symlink and leaked an out-of-root file")
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
