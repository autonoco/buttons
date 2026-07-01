package store

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// makeTarball builds a gz tarball wrapping files under <folder>/ — the layout
// tools/publish.mjs produces (the on-disk button folder name is the wrapper).
func makeTarball(t *testing.T, folder string, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	// a directory entry, like tar -C src folder produces
	if err := tw.WriteHeader(&tar.Header{Name: folder + "/", Mode: 0755, Typeflag: tar.TypeDir}); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: folder + "/" + name, Mode: 0644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
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

func sha(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// registryServer mocks the Worker: /v1/index + the bearer-gated download route,
// setting x-content-sha256 to the tarball hash like the real Worker does.
func registryServer(t *testing.T, key string, entries []indexEntry, tarballs map[string][]byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+key {
			http.Error(w, `{"error":{"code":"UNAUTHORIZED","message":"valid Bearer key required"}}`, http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/v1/index":
			_ = json.NewEncoder(w).Encode(entries)
		case strings.HasPrefix(r.URL.Path, "/v1/buttons/") && strings.HasSuffix(r.URL.Path, "/download"):
			mid := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/buttons/"), "/download")
			i := strings.LastIndex(mid, "/") // split name / version on the last slash
			name := mid[:i]
			tb, ok := tarballs[name]
			if !ok {
				http.Error(w, `{"error":{"code":"NOT_FOUND","message":"no button"}}`, http.StatusNotFound)
				return
			}
			w.Header().Set("X-Content-Sha256", sha(tb))
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(tb)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestHTTPSourceIndexAndFetch(t *testing.T) {
	const key = "test-key"
	files := map[string]string{
		"button.json": `{"schema_version":2,"name":"hello","runtime":"shell","requires":["@autono/dep"]}`,
		"main.sh":     "echo hi\n",
		"AGENTS.md":   "# hello\n",
	}
	tb := makeTarball(t, "hello", files)
	entries := []indexEntry{{Name: "@autono/hello", Kind: "button", Version: "1", SHA256: sha(tb)}}
	srv := registryServer(t, key, entries, map[string][]byte{"@autono/hello": tb})
	defer srv.Close()

	src := &HTTPSource{BaseURL: srv.URL, Key: key}

	refs, err := src.Index()
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	if len(refs) != 1 || refs[0].Name != "@autono/hello" || refs[0].Version != "1" {
		t.Fatalf("unexpected refs: %+v", refs)
	}

	// version "" → resolves latest from the index
	b, err := src.Fetch("@autono/hello", "")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if b.Version != "1" {
		t.Errorf("version = %q, want 1", b.Version)
	}
	if got := string(b.Files["main.sh"]); got != "echo hi\n" {
		t.Errorf("main.sh = %q", got)
	}
	if _, ok := b.Files["button.json"]; !ok {
		t.Error("bundle missing button.json")
	}
	// keys are flattened (the wrapping "hello/" stripped), so install writes them
	// directly into the button dir.
	for k := range b.Files {
		if strings.Contains(k, "/") {
			t.Errorf("file key not flattened: %q", k)
		}
	}
	// Bundle.SHA256 is the file-content hash (stamped as content_hash), not the tarball hash.
	if b.SHA256 != hashFiles(b.Files) {
		t.Error("Bundle.SHA256 should equal hashFiles(Files)")
	}
	if b.SHA256 == sha(tb) {
		t.Error("Bundle.SHA256 must differ from the tarball hash")
	}
}

func TestHTTPSourceRejectsHashMismatch(t *testing.T) {
	const key = "k"
	tb := makeTarball(t, "x", map[string]string{"button.json": `{"name":"x"}`})
	// Server streams the real tarball but advertises a WRONG x-content-sha256.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+key {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		w.Header().Set("X-Content-Sha256", "00deadbeef")
		_, _ = w.Write(tb)
	}))
	defer srv.Close()

	src := &HTTPSource{BaseURL: srv.URL, Key: key}
	_, err := src.Fetch("x", "1") // pinned version → goes straight to download
	if err == nil || !strings.Contains(err.Error(), "content hash mismatch") {
		t.Fatalf("expected content hash mismatch, got %v", err)
	}
}

func TestHTTPSourceAuthFailure(t *testing.T) {
	srv := registryServer(t, "right-key", nil, nil)
	defer srv.Close()
	src := &HTTPSource{BaseURL: srv.URL, Key: "wrong-key"}
	if _, err := src.Index(); err == nil {
		t.Fatal("expected auth failure with wrong key")
	}
}

func TestHTTPSourceUsesContextForRequests(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.NotFound(w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	src := &HTTPSource{BaseURL: srv.URL, Client: srv.Client(), Context: ctx}
	_, err := src.Fetch("@autono/hello", "1")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Fetch error = %v, want context canceled", err)
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("registry request count = %d, want 0", got)
	}
}

func TestHTTPSourceRejectsTraversalEntries(t *testing.T) {
	tb := makeTarball(t, "hello", map[string]string{
		"button.json": `{"name":"hello"}`,
		"../evil":     "bad",
	})
	_, err := untarGz(tb)
	if err == nil || !strings.Contains(err.Error(), "unsafe path in artifact") {
		t.Fatalf("untarGz error = %v, want unsafe path", err)
	}
}

func TestHTTPSourceSkipsMacJunk(t *testing.T) {
	const key = "k"
	files := map[string]string{
		"button.json":   `{"name":"hello"}`,
		"main.sh":       "echo hi\n",
		"._button.json": "appledouble-junk", // macOS `tar` xattr sidecar
		".DS_Store":     "finder-junk",
	}
	tb := makeTarball(t, "hello", files)
	entries := []indexEntry{{Name: "@autono/hello", Version: "1", SHA256: sha(tb)}}
	srv := registryServer(t, key, entries, map[string][]byte{"@autono/hello": tb})
	defer srv.Close()

	b, err := (&HTTPSource{BaseURL: srv.URL, Key: key}).Fetch("@autono/hello", "1")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if _, ok := b.Files["._button.json"]; ok {
		t.Error("._button.json (AppleDouble) should be skipped")
	}
	if _, ok := b.Files[".DS_Store"]; ok {
		t.Error(".DS_Store should be skipped")
	}
	if _, ok := b.Files["button.json"]; !ok {
		t.Error("button.json should be kept")
	}
	if len(b.Files) != 2 {
		t.Errorf("expected 2 real files, got %d", len(b.Files))
	}
}
