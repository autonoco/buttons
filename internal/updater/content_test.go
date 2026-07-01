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
	reg.addButton(t, "@autono/hello", "hello", "1.0.0", "#!/bin/sh\necho v1\n")
	srv := httptest.NewServer(reg.handler())
	defer srv.Close()

	installFromRegistry(t, home, srv.URL, "rk", map[string]string{"@autono/hello": "latest"})

	reg.addButton(t, "@autono/hello", "hello", "1.1.0", "#!/bin/sh\necho v2\n")
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
	if got := readInstalledButton(t, home, "hello"); got.Version != "1.1.0" {
		t.Fatalf("installed version = %q, want 1.1.0", got.Version)
	}
	lock, err := manifest.LoadLockfile()
	if err != nil {
		t.Fatal(err)
	}
	if got := lock.Dependencies["@autono/hello"].Version; got != "1.1.0" {
		t.Fatalf("lock version = %q, want 1.1.0", got)
	}
}

func TestPinnedContentDoesNotUpdate(t *testing.T) {
	home := t.TempDir()
	reg := newTestRegistry("rk")
	reg.addButton(t, "@autono/hello", "hello", "1.0.0", "#!/bin/sh\necho v1\n")
	srv := httptest.NewServer(reg.handler())
	defer srv.Close()

	installFromRegistry(t, home, srv.URL, "rk", map[string]string{"@autono/hello": "1.0.0"})
	reg.addButton(t, "@autono/hello", "hello", "1.1.0", "#!/bin/sh\necho v2\n")

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
	if got := readInstalledButton(t, home, "hello"); got.Version != "1.0.0" {
		t.Fatalf("pinned installed version = %q, want 1.0.0", got.Version)
	}
}

func TestApplyContentUpdateSkipsLocalEdits(t *testing.T) {
	home := t.TempDir()
	reg := newTestRegistry("rk")
	reg.addButton(t, "@autono/hello", "hello", "1.0.0", "#!/bin/sh\necho v1\n")
	srv := httptest.NewServer(reg.handler())
	defer srv.Close()

	installFromRegistry(t, home, srv.URL, "rk", map[string]string{"@autono/hello": "latest"})
	if err := os.WriteFile(filepath.Join(home, "buttons", "hello", "main.sh"), []byte("#!/bin/sh\necho local\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	reg.addButton(t, "@autono/hello", "hello", "1.1.0", "#!/bin/sh\necho v2\n")

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
