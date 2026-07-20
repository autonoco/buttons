package cmd

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
	"strings"
	"sync"
	"testing"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/manifest"
)

const testRegistryKey = "test-key"

// testRegistry mocks the registry Worker for cmd-level add/remove tests:
// /v1/index plus the bearer-gated download route, with x-content-sha256 set
// like the real Worker. Entries and tarballs can grow mid-test (publishing a
// new version) so floating-refresh behavior is observable.
type testRegistry struct {
	mu       sync.Mutex
	entries  []map[string]any
	tarballs map[string][]byte // "@desk/name@version" → tarball
	srv      *httptest.Server
}

func newTestRegistry(t *testing.T) *testRegistry {
	t.Helper()
	reg := &testRegistry{tarballs: map[string][]byte{}}
	reg.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testRegistryKey {
			http.Error(w, `{"error":{"code":"UNAUTHORIZED","message":"valid Bearer key required"}}`, http.StatusUnauthorized)
			return
		}
		reg.mu.Lock()
		defer reg.mu.Unlock()
		switch {
		case r.URL.Path == "/v1/index":
			_ = json.NewEncoder(w).Encode(reg.entries)
		case strings.HasPrefix(r.URL.Path, "/v1/buttons/") && strings.HasSuffix(r.URL.Path, "/download"):
			mid := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/buttons/"), "/download")
			i := strings.LastIndex(mid, "/") // split name / version on the last slash
			tb, ok := reg.tarballs[mid[:i]+"@"+mid[i+1:]]
			if !ok {
				http.Error(w, `{"error":{"code":"NOT_FOUND","message":"no button"}}`, http.StatusNotFound)
				return
			}
			w.Header().Set("X-Content-Sha256", testSHA256(tb))
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(tb)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(reg.srv.Close)
	t.Setenv("BUTTONS_REGISTRY_URL", reg.srv.URL)
	t.Setenv("BUTTONS_BAT_REGISTRY_KEY", testRegistryKey)
	return reg
}

// publishButton adds a shell button version to the mock registry index.
func (reg *testRegistry) publishButton(t *testing.T, name, version string, requires map[string]string) {
	t.Helper()
	local := name[strings.LastIndex(name, "/")+1:]
	spec := button.Button{SchemaVersion: 1, Name: local, Runtime: "shell", Version: version, Requires: requires}
	data, err := json.MarshalIndent(&spec, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	tb := testTarball(t, local, map[string][]byte{
		"button.json": data,
		"main.sh":     []byte("#!/bin/sh\necho " + local + "\n"),
	})
	reg.mu.Lock()
	defer reg.mu.Unlock()
	reg.tarballs[name+"@"+version] = tb
	reg.entries = append(reg.entries, map[string]any{"name": name, "kind": "button", "version": version, "sha256": testSHA256(tb)})
}

// testTarball builds a gz tarball wrapping files under <folder>/ — the layout
// tools/publish.mjs produces (the on-disk button folder name is the wrapper).
func testTarball(t *testing.T, folder string, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: folder + "/", Mode: 0o755, Typeflag: tar.TypeDir}); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: folder + "/" + name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
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

func testSHA256(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func TestRunAddRefreshFloating(t *testing.T) {
	cases := []struct {
		name            string
		refreshFloating bool // what --no-refresh toggles off
		wantHello       string
	}{
		{name: "no-refresh keeps floating deps at locked versions", refreshFloating: false, wantHello: "1"},
		{name: "default refresh re-resolves floating deps", refreshFloating: true, wantHello: "2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("BUTTONS_HOME", t.TempDir())
			reg := newTestRegistry(t)
			reg.publishButton(t, "@autono/hello", "1", nil)
			if _, _, _, err := runAdd(context.Background(), "@autono/hello", true); err != nil {
				t.Fatalf("add hello: %v", err)
			}
			reg.publishButton(t, "@autono/hello", "2", nil) // a newer floating candidate
			reg.publishButton(t, "@autono/pinned", "1", nil)

			if _, _, _, err := runAdd(context.Background(), "@autono/pinned@1", tc.refreshFloating); err != nil {
				t.Fatalf("add pinned: %v", err)
			}

			lock, err := manifest.LoadLockfile()
			if err != nil {
				t.Fatal(err)
			}
			if got := lock.Dependencies["@autono/hello"].Version; got != tc.wantHello {
				t.Fatalf("hello locked version = %q, want %q", got, tc.wantHello)
			}
			if got := lock.Dependencies["@autono/pinned"].Version; got != "1" {
				t.Fatalf("pinned locked version = %q, want 1", got)
			}
		})
	}
}
