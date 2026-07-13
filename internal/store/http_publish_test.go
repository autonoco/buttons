package store

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// mockRegistry mimics the Worker's contract for both halves of the loop: a
// write-key-gated POST that verifies the artifact hash + enforces version
// immutability, and the read-key-gated GET index/download HTTPSource installs
// from. Enough to round-trip publish → fetch entirely through the real clients.
type mockRegistry struct {
	readKey, writeKey string
	mu                sync.Mutex
	entries           []indexEntry
	tarballs          map[string][]byte // "name@version" -> bytes
}

func newMockRegistry(readKey, writeKey string) *mockRegistry {
	return &mockRegistry{readKey: readKey, writeKey: writeKey, tarballs: map[string][]byte{}}
}

func (m *mockRegistry) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v1/buttons/"):
			if r.Header.Get("Authorization") != "Bearer "+m.writeKey {
				http.Error(w, `{"error":{"code":"UNAUTHORIZED","message":"write key required"}}`, http.StatusUnauthorized)
				return
			}
			// /v1/buttons/<name>/<version> — split version off the last slash
			// (name itself contains a slash, e.g. @autono/hello).
			mid := strings.TrimPrefix(r.URL.Path, "/v1/buttons/")
			i := strings.LastIndex(mid, "/")
			if i < 0 {
				http.Error(w, `{"error":{"code":"BAD_PATH","message":"want /v1/buttons/<name>/<version>"}}`, http.StatusBadRequest)
				return
			}
			name, version := mid[:i], mid[i+1:]
			body, _ := io.ReadAll(r.Body)
			if declared := r.Header.Get("X-Content-Sha256"); declared != sha(body) {
				http.Error(w, `{"error":{"code":"HASH_MISMATCH","message":"declared hash != bytes"}}`, http.StatusBadRequest)
				return
			}
			m.mu.Lock()
			defer m.mu.Unlock()
			for _, e := range m.entries {
				if e.Name == name && e.Version == version {
					http.Error(w, `{"error":{"code":"VERSION_EXISTS","message":"versions are immutable"}}`, http.StatusConflict)
					return
				}
			}
			m.entries = append(m.entries, indexEntry{
				Name: name, Kind: r.Header.Get("X-Button-Kind"), Version: version, SHA256: sha(body),
			})
			m.tarballs[name+"@"+version] = body
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"ok":true}`))

		case r.Method == http.MethodGet && r.URL.Path == "/v1/index":
			if r.Header.Get("Authorization") != "Bearer "+m.readKey {
				http.Error(w, `{"error":{"code":"UNAUTHORIZED","message":"read key required"}}`, http.StatusUnauthorized)
				return
			}
			m.mu.Lock()
			defer m.mu.Unlock()
			_ = json.NewEncoder(w).Encode(m.entries)

		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/buttons/") && strings.HasSuffix(r.URL.Path, "/download"):
			if r.Header.Get("Authorization") != "Bearer "+m.readKey {
				http.Error(w, `{"error":{"code":"UNAUTHORIZED","message":"read key required"}}`, http.StatusUnauthorized)
				return
			}
			mid := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/buttons/"), "/download")
			i := strings.LastIndex(mid, "/")
			name, version := mid[:i], mid[i+1:]
			m.mu.Lock()
			tb, ok := m.tarballs[name+"@"+version]
			m.mu.Unlock()
			if !ok {
				http.Error(w, `{"error":{"code":"NOT_FOUND","message":"no artifact"}}`, http.StatusNotFound)
				return
			}
			w.Header().Set("X-Content-Sha256", sha(tb))
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(tb)

		default:
			http.NotFound(w, r)
		}
	})
}

func sampleBundle() (map[string][]byte, *Bundle) {
	files := map[string][]byte{
		"button.json": []byte(`{"schema_version":1,"name":"hello","runtime":"shell","version":"1"}`),
		"main.sh":     []byte("#!/bin/sh\necho hi\n"),
		"AGENTS.md":   []byte("# hello\n"),
	}
	return files, &Bundle{Name: "@autono/hello", Version: "1", SHA256: hashFiles(files), Files: files}
}

// TestPublishThenFetchRoundTrip is the whole point: an artifact published by
// HTTPPublisher is byte-for-byte installable by HTTPSource, and the content hash
// install would pin equals the one the local bundle had.
func TestPublishThenFetchRoundTrip(t *testing.T) {
	reg := newMockRegistry("rk", "wk")
	srv := httptest.NewServer(reg.handler())
	defer srv.Close()

	files, b := sampleBundle()
	if err := (&HTTPPublisher{BaseURL: srv.URL, Key: "wk"}).Publish(b); err != nil {
		t.Fatalf("publish: %v", err)
	}

	got, err := (&HTTPSource{BaseURL: srv.URL, Key: "rk"}).Fetch("@autono/hello", "1")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got.SHA256 != hashFiles(files) {
		t.Fatalf("round-trip content hash mismatch: fetched %s, published %s", got.SHA256, hashFiles(files))
	}
	if string(got.Files["main.sh"]) != "#!/bin/sh\necho hi\n" {
		t.Errorf("main.sh round-trip wrong: %q", got.Files["main.sh"])
	}
	for k := range got.Files {
		if strings.Contains(k, "/") {
			t.Errorf("file key not flattened (wrapper not stripped): %q", k)
		}
	}
	// version "" must also resolve to the just-published one via the index.
	latest, err := (&HTTPSource{BaseURL: srv.URL, Key: "rk"}).Fetch("@autono/hello", "")
	if err != nil || latest.Version != "1" {
		t.Fatalf("latest resolve: ver=%q err=%v", latest.Version, err)
	}
}

func TestHTTPPublisherRejectsDuplicateVersion(t *testing.T) {
	reg := newMockRegistry("rk", "wk")
	srv := httptest.NewServer(reg.handler())
	defer srv.Close()

	_, b := sampleBundle()
	pub := &HTTPPublisher{BaseURL: srv.URL, Key: "wk"}
	if err := pub.Publish(b); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	err := pub.Publish(b)
	if err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("re-publishing a version should conflict, got %v", err)
	}
}

func TestHTTPPublisherAuthFailure(t *testing.T) {
	reg := newMockRegistry("rk", "wk")
	srv := httptest.NewServer(reg.handler())
	defer srv.Close()

	_, b := sampleBundle()
	if err := (&HTTPPublisher{BaseURL: srv.URL, Key: "wrong"}).Publish(b); err == nil {
		t.Fatal("expected auth failure with the wrong write key")
	}
}

func TestHTTPPublisherRequiresNameAndVersion(t *testing.T) {
	pub := &HTTPPublisher{BaseURL: "http://unused.invalid", Key: "wk"}
	if err := pub.Publish(&Bundle{Name: "@autono/hello", Files: map[string][]byte{"button.json": []byte("{}")}}); err == nil {
		t.Error("missing version should error before any network call")
	}
	if err := pub.Publish(&Bundle{Version: "1", Files: map[string][]byte{"button.json": []byte("{}")}}); err == nil {
		t.Error("missing name should error before any network call")
	}
}

func TestHTTPPublisherSendsFlowDefinitionMetadata(t *testing.T) {
	definition := []byte(`{"schema_version":2,"name":"software-delivery","drawer_kind":"flow"}`)
	var drawerKind, definitionHash string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		drawerKind = r.Header.Get("X-Drawer-Kind")
		definitionHash = r.Header.Get("X-Flow-Definition-Sha256")
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	bundle := &Bundle{
		Name:                 "@autono/software-delivery",
		Kind:                 "drawer",
		Version:              "1",
		Files:                map[string][]byte{"drawer.json": []byte(`{"schema_version":2}`), "flow-definition.json": definition},
		FlowDefinition:       definition,
		FlowDefinitionSHA256: sha256hex(definition),
	}
	if err := (&HTTPPublisher{BaseURL: srv.URL}).Publish(bundle); err != nil {
		t.Fatalf("publish flow bundle: %v", err)
	}
	if drawerKind != "flow" {
		t.Fatalf("X-Drawer-Kind = %q, want flow", drawerKind)
	}
	if definitionHash != sha256hex(definition) {
		t.Fatalf("X-Flow-Definition-Sha256 = %q, want %q", definitionHash, sha256hex(definition))
	}
}

func TestHTTPPublisherRejectsMismatchedFlowDefinitionHash(t *testing.T) {
	bundle := &Bundle{
		Name:                 "@autono/software-delivery",
		Kind:                 "drawer",
		Version:              "1",
		Files:                map[string][]byte{"drawer.json": []byte(`{"schema_version":2}`)},
		FlowDefinition:       []byte(`{"drawer_kind":"flow"}`),
		FlowDefinitionSHA256: "wrong",
	}
	err := (&HTTPPublisher{BaseURL: "http://unused.invalid"}).Publish(bundle)
	if err == nil || !strings.Contains(err.Error(), "flow definition hash mismatch") {
		t.Fatalf("Publish() error = %v, want flow definition hash mismatch", err)
	}
}

// TestTarGzUntarGzRoundTrip pins the artifact contract: tarGz wraps, untarGz
// strips, content survives. This is what makes publish → install lossless.
func TestTarGzUntarGzRoundTrip(t *testing.T) {
	files := map[string][]byte{
		"button.json": []byte(`{"name":"x"}`),
		"main.sh":     []byte("echo hi\n"),
		"AGENTS.md":   []byte("# x\n"),
	}
	tb, err := tarGz("x", files)
	if err != nil {
		t.Fatalf("tarGz: %v", err)
	}
	got, err := untarGz(tb)
	if err != nil {
		t.Fatalf("untarGz: %v", err)
	}
	if hashFiles(got) != hashFiles(files) {
		t.Fatalf("round trip changed content: got %v want %v", got, files)
	}
}

func TestSplitScoped(t *testing.T) {
	ok := func(ref, wantDesk, wantName string) {
		t.Helper()
		d, n, err := splitScoped(ref)
		if err != nil || d != wantDesk || n != wantName {
			t.Errorf("splitScoped(%q) = (%q,%q,%v), want (%q,%q,nil)", ref, d, n, err, wantDesk, wantName)
		}
	}
	bad := func(ref string) {
		t.Helper()
		if _, _, err := splitScoped(ref); err == nil {
			t.Errorf("splitScoped(%q) should have errored", ref)
		}
	}
	ok("@autono/hello", "@autono", "hello")
	bad("hello")    // unscoped
	bad("@autono")  // no name
	bad("@autono/") // empty name
	bad("@/hello")  // empty desk
	bad("@a/b/c")   // name has a slash
}
