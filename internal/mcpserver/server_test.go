package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeButton(t *testing.T, home, name string, mcpEnabled bool, body string) {
	t.Helper()
	dir := filepath.Join(home, "buttons", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	enabled := "false"
	if mcpEnabled {
		enabled = "true"
	}
	spec := `{"schema_version":1,"name":"` + name + `","runtime":"shell","env":{},"timeout_seconds":30,"mcp_enabled":` + enabled + `,"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, "button.json"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.sh"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// resultText pulls the first content text block out of a tool result map.
func resultText(t *testing.T, res map[string]any) (string, bool) {
	t.Helper()
	content, _ := res["content"].([]map[string]any)
	if len(content) == 0 {
		t.Fatalf("no content in result: %v", res)
	}
	isErr, _ := res["isError"].(bool)
	return content[0]["text"].(string), isErr
}

func TestToolsListOnlyMCPEnabled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	writeButton(t, home, "exposed", true, "#!/bin/sh\necho hi\n")
	writeButton(t, home, "hidden", false, "#!/bin/sh\necho no\n")

	s := New(Config{})
	text, isErr := resultText(t, s.toolList(nil))
	if isErr {
		t.Fatalf("list errored: %s", text)
	}
	if !strings.Contains(text, "exposed") || strings.Contains(text, "hidden") {
		t.Fatalf("list must include only mcp_enabled buttons, got: %s", text)
	}
}

func TestPressForbiddenWhenNotEnabled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	writeButton(t, home, "hidden", false, "#!/bin/sh\necho no\n")

	s := New(Config{})
	text, isErr := resultText(t, s.toolPress(context.Background(), json.RawMessage(`{"name":"hidden"}`)))
	if !isErr || !strings.Contains(text, "FORBIDDEN") {
		t.Fatalf("expected FORBIDDEN, got isErr=%v text=%s", isErr, text)
	}
}

func TestPressExecutesEnabled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	writeButton(t, home, "echo", true, "#!/bin/sh\necho pressed\n")

	s := New(Config{})
	text, isErr := resultText(t, s.toolPress(context.Background(), json.RawMessage(`{"name":"echo"}`)))
	if isErr || !strings.Contains(text, "pressed") {
		t.Fatalf("press should succeed with stdout, got isErr=%v text=%s", isErr, text)
	}
}

func TestPressRateLimited(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	writeButton(t, home, "echo", true, "#!/bin/sh\necho ok\n")

	s := New(Config{RateLimitPerMin: 2})
	_, _ = resultText(t, s.toolPress(context.Background(), json.RawMessage(`{"name":"echo"}`)))
	_, _ = resultText(t, s.toolPress(context.Background(), json.RawMessage(`{"name":"echo"}`)))
	text, isErr := resultText(t, s.toolPress(context.Background(), json.RawMessage(`{"name":"echo"}`)))
	if !isErr || !strings.Contains(text, "RATE_LIMITED") {
		t.Fatalf("3rd call under limit 2 should be RATE_LIMITED, got isErr=%v text=%s", isErr, text)
	}
}

func TestConcurrencyGuard(t *testing.T) {
	s := New(Config{})
	if !s.acquire("x") {
		t.Fatal("first acquire should succeed")
	}
	if s.acquire("x") {
		t.Fatal("second acquire should fail (busy)")
	}
	s.release("x")
	if !s.acquire("x") {
		t.Fatal("acquire after release should succeed")
	}
}

func TestInspectReturnsSpec(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	writeButton(t, home, "echo", true, "#!/bin/sh\necho ok\n")

	s := New(Config{})
	text, isErr := resultText(t, s.toolInspect(json.RawMessage(`{"name":"echo"}`)))
	if isErr || !strings.Contains(text, `"recent_runs"`) || !strings.Contains(text, "echo") {
		t.Fatalf("inspect should return spec+runs, got isErr=%v text=%s", isErr, text)
	}
}

func TestCreateGatedByFlag(t *testing.T) {
	t.Setenv("BUTTONS_HOME", t.TempDir())
	// Without --allow-create the tool isn't even listed, and a call is rejected.
	s := New(Config{})
	resp := s.handleToolsCall(context.Background(), &rpcRequest{
		Method: "tools/call",
		Params: json.RawMessage(`{"name":"buttons_create","arguments":{"name":"x","code":"echo hi"}}`),
	})
	res := resp.Result.(map[string]any)
	text, isErr := resultText(t, res)
	if !isErr || !strings.Contains(text, "FORBIDDEN") {
		t.Fatalf("create should be forbidden without --allow-create, got %s", text)
	}
}

// TestServeWire drives the full stdio loop with newline-delimited JSON-RPC and
// checks initialize + tools/list responses come back id-matched.
func TestServeWire(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	writeButton(t, home, "exposed", true, "#!/bin/sh\necho hi\n")

	in := strings.NewReader(strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	}, "\n") + "\n")
	var out bytes.Buffer

	if err := New(Config{}).Serve(context.Background(), in, &out); err != nil {
		t.Fatalf("serve: %v", err)
	}

	byID := map[float64]map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var resp map[string]any
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("bad response line %q: %v", line, err)
		}
		if id, ok := resp["id"].(float64); ok {
			byID[id] = resp
		}
	}

	if len(byID) != 2 {
		t.Fatalf("want 2 id-matched responses (notification gets none), got %d: %s", len(byID), out.String())
	}
	initRes := byID[1]["result"].(map[string]any)
	if initRes["protocolVersion"] != "2025-06-18" {
		t.Fatalf("initialize should echo client protocol version, got %v", initRes["protocolVersion"])
	}
	tools := byID[2]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 3 {
		t.Fatalf("want 3 default tools, got %d", len(tools))
	}
}
