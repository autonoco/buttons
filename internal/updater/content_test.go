package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/drawer"
	"github.com/autonoco/buttons/internal/manifest"
	"github.com/autonoco/buttons/internal/store"
)

func contextWithTestDeadline(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

type testRegistry struct {
	key     string
	mu      sync.Mutex
	entries []registryEntry
	blobs   map[string][]byte
}

type registryEntry struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

func newTestRegistry(key string) *testRegistry {
	return &testRegistry{key: key, blobs: map[string][]byte{}}
}

func (r *testRegistry) addButton(t *testing.T, pkg, localName, version, body string) {
	t.Helper()
	spec := button.Button{SchemaVersion: 1, Name: localName, Runtime: "shell", Version: version}
	data, err := json.MarshalIndent(&spec, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"button.json": append(data, '\n'),
		"main.sh":     []byte(body),
	}
	tb := makeRegistryTarball(t, localName, files)
	sum := sha(tb)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, registryEntry{Name: pkg, Kind: "button", Version: version, SHA256: sum})
	r.blobs[pkg+"@"+version] = tb
}

func (r *testRegistry) addDrawer(t *testing.T, pkg, localName, version string, steps []drawer.Step) {
	t.Helper()
	spec := drawer.Drawer{SchemaVersion: drawer.SchemaVersion, Name: localName, Version: version, Steps: steps}
	data, err := json.MarshalIndent(&spec, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"drawer.json": append(data, '\n'),
	}
	tb := makeRegistryTarball(t, localName, files)
	sum := sha(tb)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, registryEntry{Name: pkg, Kind: "drawer", Version: version, SHA256: sum})
	r.blobs[pkg+"@"+version] = tb
}

func (r *testRegistry) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != "Bearer "+r.key {
			http.Error(w, `{"error":{"code":"UNAUTHORIZED","message":"read key required"}}`, http.StatusUnauthorized)
			return
		}
		switch {
		case req.Method == http.MethodGet && req.URL.Path == "/v1/index":
			r.mu.Lock()
			defer r.mu.Unlock()
			_ = json.NewEncoder(w).Encode(r.entries)
		case req.Method == http.MethodGet && strings.HasPrefix(req.URL.Path, "/v1/buttons/") && strings.HasSuffix(req.URL.Path, "/download"):
			mid := strings.TrimSuffix(strings.TrimPrefix(req.URL.Path, "/v1/buttons/"), "/download")
			i := strings.LastIndex(mid, "/")
			name, version := mid[:i], mid[i+1:]
			r.mu.Lock()
			tb, ok := r.blobs[name+"@"+version]
			r.mu.Unlock()
			if !ok {
				http.NotFound(w, req)
				return
			}
			w.Header().Set("X-Content-Sha256", sha(tb))
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(tb)
		default:
			http.NotFound(w, req)
		}
	})
}

func makeRegistryTarball(t *testing.T, wrapper string, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, data := range files {
		hdr := &tar.Header{
			Name:     path.Join(wrapper, name),
			Mode:     0o644,
			Size:     int64(len(data)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sha(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func readInstalledButton(t *testing.T, home, name string) button.Button {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, "buttons", name, "button.json"))
	if err != nil {
		t.Fatal(err)
	}
	var b button.Button
	if err := json.Unmarshal(data, &b); err != nil {
		t.Fatal(err)
	}
	return b
}

func readInstalledDrawer(t *testing.T, home, name string) drawer.Drawer {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, "drawers", name, "drawer.json"))
	if err != nil {
		t.Fatal(err)
	}
	var d drawer.Drawer
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func installFromRegistry(t *testing.T, home, url, key string, deps map[string]string) {
	t.Helper()
	t.Setenv("BUTTONS_HOME", home)
	m := &manifest.Manifest{SchemaVersion: 1, Dependencies: deps}
	if err := manifest.Save(m); err != nil {
		t.Fatalf("save manifest: %v", err)
	}
	src := &store.HTTPSource{BaseURL: url, Key: key}
	res, lock, err := store.InstallManifest(src, m, nil, store.InstallOptions{RefreshFloating: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if len(res.Installed) == 0 {
		t.Fatal("expected install to write at least one button")
	}
	if err := manifest.SaveLockfile(lock); err != nil {
		t.Fatalf("save lock: %v", err)
	}
}

func TestApplyContentUpdateFromManifest(t *testing.T) {
	home := t.TempDir()
	reg := newTestRegistry("rk")
	reg.addButton(t, "@autono/hello", "hello", "1", "#!/bin/sh\necho v1\n")
	srv := httptest.NewServer(reg.handler())
	defer srv.Close()

	installFromRegistry(t, home, srv.URL, "rk", map[string]string{"@autono/hello": "latest"})

	reg.addButton(t, "@autono/hello", "hello", "2", "#!/bin/sh\necho v2\n")
	report, err := Check(contextWithTestDeadline(t), Options{SkipBinary: true, RegistryURL: srv.URL, RegistryKey: "rk", Client: srv.Client()})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(report.Buttons) != 1 || !report.Buttons[0].UpdateAvailable {
		t.Fatalf("expected one available content update, got %+v", report.Buttons)
	}

	result, err := Apply(contextWithTestDeadline(t), Options{SkipBinary: true, RegistryURL: srv.URL, RegistryKey: "rk", Client: srv.Client()})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(result.UpdatedButtons) != 1 || result.UpdatedButtons[0] != "hello" {
		t.Fatalf("updated buttons = %v, want [hello]", result.UpdatedButtons)
	}
	if got := readInstalledButton(t, home, "hello"); got.Version != "2" {
		t.Fatalf("installed version = %q, want 2", got.Version)
	}
	lock, err := manifest.LoadLockfile()
	if err != nil {
		t.Fatal(err)
	}
	if got := lock.Dependencies["@autono/hello"].Version; got != "2" {
		t.Fatalf("lock version = %q, want 2", got)
	}
}

func TestApplyContentUpdateFromDrawerManifest(t *testing.T) {
	home := t.TempDir()
	reg := newTestRegistry("rk")
	reg.addButton(t, "@autono/build", "build", "1", "#!/bin/sh\necho build\n")
	reg.addDrawer(t, "@autono/deploy-pack", "deploy-pack", "1", []drawer.Step{{ID: "build", Button: "build"}})
	srv := httptest.NewServer(reg.handler())
	defer srv.Close()

	installFromRegistry(t, home, srv.URL, "rk", map[string]string{"@autono/deploy-pack": "latest"})

	reg.addDrawer(t, "@autono/deploy-pack", "deploy-pack", "2", []drawer.Step{{ID: "build", Button: "build"}})
	report, err := Check(contextWithTestDeadline(t), Options{SkipBinary: true, RegistryURL: srv.URL, RegistryKey: "rk", Client: srv.Client()})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	var drawerReport *ButtonReport
	for i := range report.Buttons {
		if report.Buttons[i].PackageName == "@autono/deploy-pack" {
			drawerReport = &report.Buttons[i]
			break
		}
	}
	if drawerReport == nil || drawerReport.Kind != "drawer" || !drawerReport.UpdateAvailable {
		t.Fatalf("expected one drawer content update, got %+v", report.Buttons)
	}

	result, err := Apply(contextWithTestDeadline(t), Options{SkipBinary: true, RegistryURL: srv.URL, RegistryKey: "rk", Client: srv.Client()})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(result.UpdatedButtons) != 1 || result.UpdatedButtons[0] != "deploy-pack" {
		t.Fatalf("updated packages = %v, want [deploy-pack]", result.UpdatedButtons)
	}
	if got := readInstalledDrawer(t, home, "deploy-pack"); got.Version != "2" {
		t.Fatalf("installed drawer version = %q, want 2", got.Version)
	}
}

func TestApplyContentUpdateFromDrawerMemberLockEntry(t *testing.T) {
	home := t.TempDir()
	reg := newTestRegistry("rk")
	reg.addButton(t, "@autono/build", "build", "1", "#!/bin/sh\necho build v1\n")
	reg.addDrawer(t, "@autono/deploy-pack", "deploy-pack", "1", []drawer.Step{{ID: "build", Button: "build"}})
	srv := httptest.NewServer(reg.handler())
	defer srv.Close()

	installFromRegistry(t, home, srv.URL, "rk", map[string]string{"@autono/deploy-pack": "latest"})

	reg.addButton(t, "@autono/build", "build", "2", "#!/bin/sh\necho build v2\n")
	reports, err := CheckContent(contextWithTestDeadline(t), Options{SkipBinary: true, RegistryURL: srv.URL, RegistryKey: "rk", Client: srv.Client()})
	if err != nil {
		t.Fatalf("check content: %v", err)
	}
	var buildReport *ButtonReport
	for i := range reports {
		if reports[i].PackageName == "@autono/build" {
			buildReport = &reports[i]
			break
		}
	}
	if buildReport == nil || buildReport.Kind != "button" || !buildReport.UpdateAvailable {
		t.Fatalf("expected drawer member button update report, got %+v", reports)
	}

	result, err := Apply(contextWithTestDeadline(t), Options{SkipBinary: true, RegistryURL: srv.URL, RegistryKey: "rk", Client: srv.Client()})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(result.UpdatedButtons) != 1 || result.UpdatedButtons[0] != "build" {
		t.Fatalf("updated packages = %v, want [build]", result.UpdatedButtons)
	}
	if got := readInstalledButton(t, home, "build"); got.Version != "2" {
		t.Fatalf("installed member button version = %q, want 2", got.Version)
	}
	lock, err := manifest.LoadLockfile()
	if err != nil {
		t.Fatal(err)
	}
	if got := lock.Dependencies["@autono/build"].Version; got != "2" {
		t.Fatalf("member button lock version = %q, want 2", got)
	}
}

func TestPinnedContentDoesNotUpdate(t *testing.T) {
	home := t.TempDir()
	reg := newTestRegistry("rk")
	reg.addButton(t, "@autono/hello", "hello", "1", "#!/bin/sh\necho v1\n")
	srv := httptest.NewServer(reg.handler())
	defer srv.Close()

	installFromRegistry(t, home, srv.URL, "rk", map[string]string{"@autono/hello": "1"})
	reg.addButton(t, "@autono/hello", "hello", "2", "#!/bin/sh\necho v2\n")

	report, err := Check(contextWithTestDeadline(t), Options{SkipBinary: true, RegistryURL: srv.URL, RegistryKey: "rk", Client: srv.Client()})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(report.Buttons) != 1 || !report.Buttons[0].Pinned || report.Buttons[0].UpdateAvailable {
		t.Fatalf("pinned dependency should not be updateable: %+v", report.Buttons)
	}
	result, err := Apply(contextWithTestDeadline(t), Options{SkipBinary: true, RegistryURL: srv.URL, RegistryKey: "rk", Client: srv.Client()})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(result.UpdatedButtons) != 0 {
		t.Fatalf("updated pinned buttons = %v, want none", result.UpdatedButtons)
	}
	if got := readInstalledButton(t, home, "hello"); got.Version != "1" {
		t.Fatalf("pinned installed version = %q, want 1", got.Version)
	}
}

func TestApplyContentUpdateSkipsLocalEdits(t *testing.T) {
	home := t.TempDir()
	reg := newTestRegistry("rk")
	reg.addButton(t, "@autono/hello", "hello", "1", "#!/bin/sh\necho v1\n")
	srv := httptest.NewServer(reg.handler())
	defer srv.Close()

	installFromRegistry(t, home, srv.URL, "rk", map[string]string{"@autono/hello": "latest"})
	if err := os.WriteFile(filepath.Join(home, "buttons", "hello", "main.sh"), []byte("#!/bin/sh\necho local\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	reg.addButton(t, "@autono/hello", "hello", "2", "#!/bin/sh\necho v2\n")

	result, err := Apply(contextWithTestDeadline(t), Options{SkipBinary: true, RegistryURL: srv.URL, RegistryKey: "rk", Client: srv.Client()})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(result.UpdatedButtons) != 0 {
		t.Fatalf("updated buttons = %v, want none", result.UpdatedButtons)
	}
	if len(result.Buttons) != 1 || !result.Buttons[0].Skipped {
		t.Fatalf("expected local edit skip, got %+v", result.Buttons)
	}
	got, err := os.ReadFile(filepath.Join(home, "buttons", "hello", "main.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "#!/bin/sh\necho local\n" {
		t.Fatalf("local edit was overwritten: %q", string(got))
	}
}

func TestApplyContentUpdateSkipsLocalDrawerEdits(t *testing.T) {
	home := t.TempDir()
	reg := newTestRegistry("rk")
	reg.addDrawer(t, "@autono/deploy-pack", "deploy-pack", "1", nil)
	srv := httptest.NewServer(reg.handler())
	defer srv.Close()

	installFromRegistry(t, home, srv.URL, "rk", map[string]string{"@autono/deploy-pack": "latest"})
	path := filepath.Join(home, "drawers", "deploy-pack", "drawer.json")
	local := readInstalledDrawer(t, home, "deploy-pack")
	local.Description = "local edit"
	data, err := json.MarshalIndent(&local, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	reg.addDrawer(t, "@autono/deploy-pack", "deploy-pack", "2", nil)

	result, err := Apply(contextWithTestDeadline(t), Options{SkipBinary: true, RegistryURL: srv.URL, RegistryKey: "rk", Client: srv.Client()})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(result.UpdatedButtons) != 0 {
		t.Fatalf("updated drawer packages = %v, want none", result.UpdatedButtons)
	}
	if len(result.Buttons) != 1 || !result.Buttons[0].Skipped || result.Buttons[0].Kind != "drawer" {
		t.Fatalf("expected local drawer edit skip, got %+v", result.Buttons)
	}
	if got := readInstalledDrawer(t, home, "deploy-pack"); got.Version != "1" || got.Description != "local edit" {
		t.Fatalf("local drawer was overwritten: %+v", got)
	}
}

func TestCheckContentHonorsContextDuringRegistryFetch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	if err := manifest.Save(&manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Dependencies:  map[string]string{"@autono/slow": "latest"},
	}); err != nil {
		t.Fatalf("save manifest: %v", err)
	}

	started := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(started) })
		<-r.Context().Done()
	}))
	defer srv.Close()

	client := srv.Client()
	client.Timeout = time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	reports, err := CheckContent(ctx, Options{RegistryURL: srv.URL, RegistryKey: "rk", Client: client})
	if err != nil {
		t.Fatalf("CheckContent: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("CheckContent took %s, want context deadline to stop registry fetch quickly", elapsed)
	}
	select {
	case <-started:
	default:
		t.Fatal("registry was not called")
	}
	if len(reports) != 1 || !strings.Contains(reports[0].Error, "context") {
		t.Fatalf("report = %+v, want context cancellation error", reports)
	}
}
