package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type otaRegistry struct {
	readKey  string
	writeKey string

	mu      sync.Mutex
	entries []otaIndexEntry
	blobs   map[string][]byte
}

type otaIndexEntry struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

func newOTARegistry(readKey, writeKey string) *otaRegistry {
	return &otaRegistry{
		readKey:  readKey,
		writeKey: writeKey,
		blobs:    map[string][]byte{},
	}
}

func (r *otaRegistry) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.Method == http.MethodPost && strings.HasPrefix(req.URL.Path, "/v1/buttons/"):
			if req.Header.Get("Authorization") != "Bearer "+r.writeKey {
				http.Error(w, `{"error":{"code":"UNAUTHORIZED","message":"write key required"}}`, http.StatusUnauthorized)
				return
			}
			mid := strings.TrimPrefix(req.URL.Path, "/v1/buttons/")
			i := strings.LastIndex(mid, "/")
			if i < 0 {
				http.Error(w, `{"error":{"code":"BAD_PATH","message":"want /v1/buttons/<name>/<version>"}}`, http.StatusBadRequest)
				return
			}
			name, version := mid[:i], mid[i+1:]
			body, _ := io.ReadAll(req.Body)
			if declared := req.Header.Get("X-Content-Sha256"); declared != otaSHA(body) {
				http.Error(w, `{"error":{"code":"HASH_MISMATCH","message":"declared hash != bytes"}}`, http.StatusBadRequest)
				return
			}
			r.mu.Lock()
			defer r.mu.Unlock()
			for _, e := range r.entries {
				if e.Name == name && e.Version == version {
					http.Error(w, `{"error":{"code":"VERSION_EXISTS","message":"versions are immutable"}}`, http.StatusConflict)
					return
				}
			}
			kind := req.Header.Get("X-Button-Kind")
			if kind == "" {
				kind = "button"
			}
			r.entries = append(r.entries, otaIndexEntry{Name: name, Kind: kind, Version: version, SHA256: otaSHA(body)})
			r.blobs[name+"@"+version] = body
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"ok":true}`))

		case req.Method == http.MethodGet && req.URL.Path == "/v1/index":
			if req.Header.Get("Authorization") != "Bearer "+r.readKey {
				http.Error(w, `{"error":{"code":"UNAUTHORIZED","message":"read key required"}}`, http.StatusUnauthorized)
				return
			}
			r.mu.Lock()
			defer r.mu.Unlock()
			_ = json.NewEncoder(w).Encode(r.entries)

		case req.Method == http.MethodGet && strings.HasPrefix(req.URL.Path, "/v1/buttons/") && strings.HasSuffix(req.URL.Path, "/download"):
			if req.Header.Get("Authorization") != "Bearer "+r.readKey {
				http.Error(w, `{"error":{"code":"UNAUTHORIZED","message":"read key required"}}`, http.StatusUnauthorized)
				return
			}
			mid := strings.TrimSuffix(strings.TrimPrefix(req.URL.Path, "/v1/buttons/"), "/download")
			i := strings.LastIndex(mid, "/")
			if i < 0 {
				http.NotFound(w, req)
				return
			}
			name, version := mid[:i], mid[i+1:]
			r.mu.Lock()
			tb, ok := r.blobs[name+"@"+version]
			r.mu.Unlock()
			if !ok {
				http.Error(w, `{"error":{"code":"NOT_FOUND","message":"no artifact"}}`, http.StatusNotFound)
				return
			}
			w.Header().Set("X-Content-Sha256", otaSHA(tb))
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(tb)

		default:
			http.NotFound(w, req)
		}
	})
}

func otaSHA(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func writeOTAPublishButton(t *testing.T, home, name, version, body string) {
	t.Helper()
	dir := filepath.Join(home, "buttons", name)
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
	if err := os.WriteFile(filepath.Join(dir, "button.json"), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.sh"), []byte(body), 0o755); err != nil {
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
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	return spec.Version
}

func readManifestDependency(t *testing.T, home, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, "buttons.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m.Dependencies[name]
}

func readLockVersion(t *testing.T, home, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, "buttons-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	var lock struct {
		Dependencies map[string]struct {
			Version string `json:"version"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		t.Fatal(err)
	}
	return lock.Dependencies[name].Version
}

func readLifecycleEvents(t *testing.T, home string) []struct {
	Action      string `json:"action"`
	PackageName string `json:"package_name,omitempty"`
	Requested   string `json:"requested,omitempty"`
} {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, "history.json"))
	if err != nil {
		t.Fatal(err)
	}
	var log struct {
		Events []struct {
			Action      string `json:"action"`
			PackageName string `json:"package_name,omitempty"`
			Requested   string `json:"requested,omitempty"`
		} `json:"events"`
	}
	if err := json.Unmarshal(data, &log); err != nil {
		t.Fatal(err)
	}
	return log.Events
}

func registryEnv(url string) []string {
	return []string{
		"BUTTONS_REGISTRY_URL=" + url,
		"BUTTONS_BAT_REGISTRY_KEY=read-key",
		"BUTTONS_NO_UPDATE=1",
	}
}

func publishEnv(url string) []string {
	return []string{
		"BUTTONS_REGISTRY_URL=" + url,
		"BUTTONS_BAT_REGISTRY_WRITE_KEY=write-key",
		"BUTTONS_NO_UPDATE=1",
	}
}

func TestUpdateRefreshesFloatingDependency(t *testing.T) {
	reg := newOTARegistry("read-key", "write-key")
	srv := httptest.NewServer(reg.handler())
	defer srv.Close()

	publisher := newTestEnv(t)
	agent := newTestEnv(t)

	writeOTAPublishButton(t, publisher.home, "hello", "1", "#!/bin/sh\necho v1\n")
	res := publisher.runWithEnv(publishEnv(srv.URL), "publish", "@autono/hello", "--json")
	if res.ExitCode != 0 {
		t.Fatalf("publish v1 failed: stdout=%s stderr=%s", res.Stdout, res.Stderr)
	}

	res = agent.runWithEnv(registryEnv(srv.URL), "add", "@autono/hello", "--json")
	if res.ExitCode != 0 {
		t.Fatalf("add failed: stdout=%s stderr=%s", res.Stdout, res.Stderr)
	}
	if got := readManifestDependency(t, agent.home, "@autono/hello"); got != "latest" {
		t.Fatalf("manifest dependency = %q, want latest", got)
	}
	if got := readInstalledVersion(t, agent.home, "hello"); got != "1" {
		t.Fatalf("installed version = %q, want 1", got)
	}
	if got := readLockVersion(t, agent.home, "@autono/hello"); got != "1" {
		t.Fatalf("lock version = %q, want 1", got)
	}
	events := readLifecycleEvents(t, agent.home)
	if len(events) != 1 || events[0].Action != "add" || events[0].PackageName != "@autono/hello" || events[0].Requested != "latest" {
		t.Fatalf("history after add = %+v, want add @autono/hello latest", events)
	}

	res = agent.runWithEnv(registryEnv(srv.URL), "install", "--json")
	if res.ExitCode != 0 {
		t.Fatalf("install failed: stdout=%s stderr=%s", res.Stdout, res.Stderr)
	}
	events = readLifecycleEvents(t, agent.home)
	if len(events) != 2 || events[1].Action != "install" {
		t.Fatalf("history after install = %+v, want second action install", events)
	}

	writeOTAPublishButton(t, publisher.home, "hello", "1", "#!/bin/sh\necho v2\n")
	res = publisher.runWithEnv(publishEnv(srv.URL), "publish", "@autono/hello", "--json")
	if res.ExitCode != 0 {
		t.Fatalf("publish v2 failed: stdout=%s stderr=%s", res.Stdout, res.Stderr)
	}

	status := agent.runWithEnv(registryEnv(srv.URL), "status", "--json")
	if status.ExitCode != 0 {
		t.Fatalf("status failed: stdout=%s stderr=%s", status.Stdout, status.Stderr)
	}
	statusJSON := parseJSON(t, status.Stdout)
	if !statusJSON.OK {
		t.Fatalf("status returned error: %+v", statusJSON.Error)
	}
	if !jsonContains(t, statusJSON.Data, `"update_available": true`) {
		t.Fatalf("status did not report an available update: %s", statusJSON.Data)
	}

	update := agent.runWithEnv(registryEnv(srv.URL), "update", "--json")
	if update.ExitCode != 0 {
		t.Fatalf("update failed: stdout=%s stderr=%s", update.Stdout, update.Stderr)
	}
	if got := readInstalledVersion(t, agent.home, "hello"); got != "2" {
		t.Fatalf("updated version = %q, want 2", got)
	}
	if got := readLockVersion(t, agent.home, "@autono/hello"); got != "2" {
		t.Fatalf("updated lock version = %q, want 2", got)
	}
	body, err := os.ReadFile(filepath.Join(agent.home, "buttons", "hello", "main.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "#!/bin/sh\necho v2\n" {
		t.Fatalf("installed content was not refreshed: %q", string(body))
	}
}

func TestUpdateDoesNotMoveExactPin(t *testing.T) {
	reg := newOTARegistry("read-key", "write-key")
	srv := httptest.NewServer(reg.handler())
	defer srv.Close()

	publisher := newTestEnv(t)
	agent := newTestEnv(t)

	writeOTAPublishButton(t, publisher.home, "hello", "1", "#!/bin/sh\necho v1\n")
	res := publisher.runWithEnv(publishEnv(srv.URL), "publish", "@autono/hello", "--json")
	if res.ExitCode != 0 {
		t.Fatalf("publish v1 failed: stdout=%s stderr=%s", res.Stdout, res.Stderr)
	}
	writeOTAPublishButton(t, publisher.home, "hello", "1", "#!/bin/sh\necho v2\n")
	res = publisher.runWithEnv(publishEnv(srv.URL), "publish", "@autono/hello", "--json")
	if res.ExitCode != 0 {
		t.Fatalf("publish v2 failed: stdout=%s stderr=%s", res.Stdout, res.Stderr)
	}

	res = agent.runWithEnv(registryEnv(srv.URL), "add", "@autono/hello@1", "--json")
	if res.ExitCode != 0 {
		t.Fatalf("add exact pin failed: stdout=%s stderr=%s", res.Stdout, res.Stderr)
	}
	if got := readManifestDependency(t, agent.home, "@autono/hello"); got != "1" {
		t.Fatalf("manifest dependency = %q, want 1", got)
	}
	if got := readInstalledVersion(t, agent.home, "hello"); got != "1" {
		t.Fatalf("installed version = %q, want 1", got)
	}

	update := agent.runWithEnv(registryEnv(srv.URL), "update", "--json")
	if update.ExitCode != 0 {
		t.Fatalf("update failed: stdout=%s stderr=%s", update.Stdout, update.Stderr)
	}
	if got := readInstalledVersion(t, agent.home, "hello"); got != "1" {
		t.Fatalf("pinned version moved to %q, want 1", got)
	}
}

func jsonContains(t *testing.T, raw json.RawMessage, needle string) bool {
	t.Helper()
	return strings.Contains(string(raw), needle)
}
