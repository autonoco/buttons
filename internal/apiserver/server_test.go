package apiserver

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeButton drops a runnable shell button into BUTTONS_HOME.
func writeButton(t *testing.T, home, name, body string) {
	t.Helper()
	dir := filepath.Join(home, "buttons", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	spec := `{"schema_version":1,"name":"` + name + `","runtime":"shell","env":{},"timeout_seconds":30,"mcp_enabled":false,"args":[{"name":"who","type":"string","required":false}],"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, "button.json"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.sh"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func do(t *testing.T, srv *Server, method, path, key, body string) (*http.Response, map[string]any) {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	var env map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	return rec.Result(), env
}

func TestServerAuthAndEndpoints(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	writeButton(t, home, "greet", "#!/bin/sh\necho \"hi ${BUTTONS_ARG_WHO:-world}\"\n")

	srv := New(Config{APIKey: "secret"})

	// Health needs no auth.
	if resp, env := do(t, srv, "GET", "/api/health", "", ""); resp.StatusCode != 200 || env["ok"] != true {
		t.Fatalf("health: status=%d env=%v", resp.StatusCode, env)
	}

	// List without a key → 401.
	if resp, _ := do(t, srv, "GET", "/api/buttons", "", ""); resp.StatusCode != 401 {
		t.Fatalf("expected 401 without key, got %d", resp.StatusCode)
	}
	// Wrong key → 401.
	if resp, _ := do(t, srv, "GET", "/api/buttons", "nope", ""); resp.StatusCode != 401 {
		t.Fatalf("expected 401 with wrong key, got %d", resp.StatusCode)
	}

	// List with the key → 200, includes our button.
	resp, env := do(t, srv, "GET", "/api/buttons", "secret", "")
	if resp.StatusCode != 200 {
		t.Fatalf("list: %d", resp.StatusCode)
	}
	data := env["data"].(map[string]any)
	if len(data["buttons"].([]any)) != 1 {
		t.Fatalf("want 1 button, got %v", data["buttons"])
	}

	// Get a missing button → 404.
	if resp, _ := do(t, srv, "GET", "/api/buttons/ghost", "secret", ""); resp.StatusCode != 404 {
		t.Fatalf("missing button should 404, got %d", resp.StatusCode)
	}

	// Press it with an arg → 200, status ok, stdout reflects the arg.
	resp, env = do(t, srv, "POST", "/api/buttons/greet/press", "secret", `{"args":{"who":"bobak"}}`)
	if resp.StatusCode != 200 {
		t.Fatalf("press: %d (%v)", resp.StatusCode, env)
	}
	result := env["data"].(map[string]any)
	if result["status"] != "ok" {
		t.Fatalf("press status: %v", result)
	}
	if out, _ := result["stdout"].(string); !strings.Contains(out, "hi bobak") {
		t.Fatalf("stdout missing arg: %q", out)
	}

	// Runs after the press → at least one record.
	resp, env = do(t, srv, "GET", "/api/buttons/greet/runs", "secret", "")
	if resp.StatusCode != 200 {
		t.Fatalf("runs: %d", resp.StatusCode)
	}
	runs := env["data"].(map[string]any)["runs"].([]any)
	if len(runs) < 1 {
		t.Fatalf("expected >=1 run after press, got %d", len(runs))
	}
}

func TestPressMissingButton404(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	srv := New(Config{}) // no auth
	resp, env := do(t, srv, "POST", "/api/buttons/nope/press", "", `{}`)
	if resp.StatusCode != 404 {
		t.Fatalf("press of missing button should 404, got %d (%v)", resp.StatusCode, env)
	}
}

func TestNoAuthConfigAllowsAccess(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	writeButton(t, home, "ping", "#!/bin/sh\necho pong\n")
	srv := New(Config{}) // empty key disables auth
	if resp, _ := do(t, srv, "GET", "/api/buttons", "", ""); resp.StatusCode != 200 {
		t.Fatalf("no-auth list should 200, got %d", resp.StatusCode)
	}
}
