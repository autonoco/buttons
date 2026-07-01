package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeOTASourceButton(t *testing.T, root, name, version, body string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	spec := map[string]any{
		"schema_version": 1,
		"name":           name,
		"runtime":        "shell",
		"version":        version,
	}
	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "button.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.sh"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readInstalledVersion(t *testing.T, home, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, "buttons", name, "button.json"))
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Version    string `json:"version"`
		SourceName string `json:"source_name"`
	}
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	if spec.SourceName != name {
		t.Fatalf("source_name = %q, want %q", spec.SourceName, name)
	}
	return spec.Version
}

func TestUpdateRefreshesInstalledButton(t *testing.T) {
	env := newTestEnv(t)
	source := t.TempDir()

	writeOTASourceButton(t, source, "hello", "1.0.0", "#!/bin/sh\necho v1\n")
	res := env.run("install", "hello", "--source", source, "--json")
	if res.ExitCode != 0 {
		t.Fatalf("install failed: %s", res.Stderr)
	}
	if got := readInstalledVersion(t, env.home, "hello"); got != "1.0.0" {
		t.Fatalf("installed version = %q, want 1.0.0", got)
	}

	writeOTASourceButton(t, source, "hello", "1.1.0", "#!/bin/sh\necho v2\n")
	status := env.run("status", "--json")
	if status.ExitCode != 0 {
		t.Fatalf("status failed: %s", status.Stderr)
	}
	statusJSON := parseJSON(t, status.Stdout)
	if !statusJSON.OK {
		t.Fatalf("status returned error: %+v", statusJSON.Error)
	}
	if !jsonContains(t, statusJSON.Data, `"update_available": true`) {
		t.Fatalf("status did not report an available update: %s", statusJSON.Data)
	}

	update := env.run("update", "--json")
	if update.ExitCode != 0 {
		t.Fatalf("update failed: stdout=%s stderr=%s", update.Stdout, update.Stderr)
	}
	if got := readInstalledVersion(t, env.home, "hello"); got != "1.1.0" {
		t.Fatalf("updated version = %q, want 1.1.0", got)
	}
}

func jsonContains(t *testing.T, raw json.RawMessage, needle string) bool {
	t.Helper()
	return strings.Contains(string(raw), needle)
}
