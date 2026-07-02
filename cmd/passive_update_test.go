package cmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/autonoco/buttons/internal/drawer"
	"github.com/autonoco/buttons/internal/manifest"
	"github.com/autonoco/buttons/internal/settings"
	"github.com/autonoco/buttons/internal/store"
	"github.com/spf13/cobra"
)

func TestPassiveUpdateSkipsCIWithoutRegistryProbe(t *testing.T) {
	oldVersion := version
	version = "1.0.0"
	t.Cleanup(func() { version = oldVersion })

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"service":"buttons-registry","min_cli_version":"2.0.0"}`))
	}))
	defer srv.Close()

	t.Setenv("BUTTONS_REGISTRY_URL", srv.URL)
	t.Setenv("BUTTONS_BAT_REGISTRY_KEY", "read")
	t.Setenv("BUTTONS_HOME", t.TempDir())
	t.Setenv("BUTTONS_NO_UPDATE", "")
	t.Setenv("CI", "true")

	maybeRunPassiveUpdate(&cobra.Command{Use: "status"})

	if got := hits.Load(); got != 0 {
		t.Fatalf("registry probe count = %d, want 0", got)
	}
}

func TestPassiveUpdateAppliesDrawerPackage(t *testing.T) {
	oldVersion := version
	version = "1"
	t.Cleanup(func() { version = oldVersion })

	oldSkip := shouldSkipPassiveUpdateFunc
	shouldSkipPassiveUpdateFunc = func() bool { return false }
	t.Cleanup(func() { shouldSkipPassiveUpdateFunc = oldSkip })

	home := t.TempDir()
	reg := newPassiveRegistry("read")
	reg.addDrawer(t, "@autono/deploy-pack", "deploy-pack", "1")
	srv := httptest.NewServer(reg.handler())
	defer srv.Close()

	t.Setenv("BUTTONS_HOME", home)
	t.Setenv("BUTTONS_REGISTRY_URL", srv.URL)
	t.Setenv("BUTTONS_BAT_REGISTRY_KEY", "read")
	t.Setenv("BUTTONS_NO_UPDATE", "")
	t.Setenv("CI", "")

	m := &manifest.Manifest{SchemaVersion: manifest.SchemaVersion, Dependencies: map[string]string{"@autono/deploy-pack": "latest"}}
	if err := manifest.Save(m); err != nil {
		t.Fatalf("save manifest: %v", err)
	}
	src := &store.HTTPSource{BaseURL: srv.URL, Key: "read", Client: srv.Client()}
	_, lock, err := store.InstallManifest(src, m, nil, store.InstallOptions{RefreshFloating: true})
	if err != nil {
		t.Fatalf("install drawer v1: %v", err)
	}
	if err := manifest.SaveLockfile(lock); err != nil {
		t.Fatalf("save lockfile: %v", err)
	}

	reg.addDrawer(t, "@autono/deploy-pack", "deploy-pack", "2")
	maybeRunPassiveUpdate(&cobra.Command{Use: "status"})

	data, err := os.ReadFile(filepath.Join(home, "drawers", "deploy-pack", "drawer.json"))
	if err != nil {
		t.Fatalf("read installed drawer: %v", err)
	}
	var got drawer.Drawer
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("parse installed drawer: %v", err)
	}
	if got.Version != "2" {
		t.Fatalf("passive update installed drawer version %q, want 2", got.Version)
	}
}

func TestPassiveUpdatePlanRunsContentInsideBinaryThrottle(t *testing.T) {
	buttonsAutoUpdate := true
	cliAutoUpdate := true
	now := time.Unix(2000, 0)
	last := now.Add(-time.Minute).Unix()
	st := &settings.Settings{
		Defaults: settings.Defaults{
			ButtonsAutoUpdate:   &buttonsAutoUpdate,
			CLIAutoUpdate:       &cliAutoUpdate,
			LastUpdateCheckUnix: &last,
		},
	}

	plan := passiveUpdatePlan(st, false, now)
	if !plan.run {
		t.Fatal("passive content update should run even when binary check is throttled")
	}
	if !plan.skipBinary {
		t.Fatal("binary update should stay throttled")
	}
	if plan.recordCheck {
		t.Fatal("content-only passive update should not refresh binary throttle timestamp")
	}
}

type passiveRegistry struct {
	key     string
	entries []passiveRegistryEntry
	blobs   map[string][]byte
}

type passiveRegistryEntry struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

func newPassiveRegistry(key string) *passiveRegistry {
	return &passiveRegistry{key: key, blobs: map[string][]byte{}}
}

func (r *passiveRegistry) addDrawer(t *testing.T, pkg, localName, version string) {
	t.Helper()
	spec := drawer.Drawer{SchemaVersion: drawer.SchemaVersion, Name: localName, Version: version}
	data, err := json.MarshalIndent(&spec, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{"drawer.json": append(data, '\n')}
	blob := passiveTarball(t, localName, files)
	sum := passiveSHA(blob)
	r.entries = append(r.entries, passiveRegistryEntry{Name: pkg, Kind: "drawer", Version: version, SHA256: sum})
	r.blobs[pkg+"@"+version] = blob
}

func (r *passiveRegistry) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != "Bearer "+r.key {
			http.Error(w, `{"error":{"code":"UNAUTHORIZED","message":"read key required"}}`, http.StatusUnauthorized)
			return
		}
		switch {
		case req.Method == http.MethodGet && req.URL.Path == "/v1/meta":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"service":"buttons-registry"}`))
		case req.Method == http.MethodGet && req.URL.Path == "/v1/index":
			_ = json.NewEncoder(w).Encode(r.entries)
		case req.Method == http.MethodGet && strings.HasPrefix(req.URL.Path, "/v1/buttons/") && strings.HasSuffix(req.URL.Path, "/download"):
			mid := req.URL.Path[len("/v1/buttons/"):]
			mid = mid[:len(mid)-len("/download")]
			i := lastSlash(mid)
			if i < 0 {
				http.NotFound(w, req)
				return
			}
			name, ver := mid[:i], mid[i+1:]
			blob, ok := r.blobs[name+"@"+ver]
			if !ok {
				http.NotFound(w, req)
				return
			}
			w.Header().Set("X-Content-Sha256", passiveSHA(blob))
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(blob)
		default:
			http.NotFound(w, req)
		}
	})
}

func passiveTarball(t *testing.T, wrapper string, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, data := range files {
		hdr := &tar.Header{Name: path.Join(wrapper, name), Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}
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

func passiveSHA(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func lastSlash(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}

func TestPassiveUpdatePlanRunsButtonsWhenCLIAutoUpdateDisabled(t *testing.T) {
	buttonsAutoUpdate := true
	cliAutoUpdate := false
	st := &settings.Settings{
		Defaults: settings.Defaults{
			ButtonsAutoUpdate: &buttonsAutoUpdate,
			CLIAutoUpdate:     &cliAutoUpdate,
		},
	}

	plan := passiveUpdatePlan(st, false, time.Unix(2000, 0))
	if !plan.run {
		t.Fatal("passive button update should run")
	}
	if !plan.skipBinary {
		t.Fatal("CLI binary update should be skipped")
	}
	if plan.skipContent {
		t.Fatal("button content update should not be skipped")
	}
	if plan.recordCheck {
		t.Fatal("button-only passive update should not refresh CLI throttle timestamp")
	}
}
