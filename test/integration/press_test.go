package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type pressResult struct {
	Status     string            `json:"status"`
	ExitCode   int               `json:"exit_code"`
	Stdout     string            `json:"stdout"`
	Stderr     string            `json:"stderr"`
	DurationMs int64             `json:"duration_ms"`
	ErrorType  string            `json:"error_type,omitempty"`
	Button     string            `json:"button"`
	Args       map[string]string `json:"args,omitempty"`
	StartedAt  string            `json:"started_at"`
}

func parsePressResult(t *testing.T, data json.RawMessage) pressResult {
	t.Helper()
	var r pressResult
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatalf("failed to parse press result: %v", err)
	}
	return r
}

func (e *testEnv) createScriptWithContent(name, content string) string {
	e.t.Helper()
	path := filepath.Join(e.home, name)
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		e.t.Fatalf("failed to create script: %v", err)
	}
	return path
}

func (e *testEnv) createButton(name, scriptPath string, extraArgs ...string) {
	e.t.Helper()
	args := append([]string{"create", name, "--file", scriptPath, "--json"}, extraArgs...)
	res := e.run(args...)
	if res.ExitCode != 0 {
		e.t.Fatalf("failed to create button %q: %s", name, res.Stderr)
	}
}

func TestPress_Success(t *testing.T) {
	env := newTestEnv(t)
	script := env.createScriptWithContent("hello.sh", "#!/bin/sh\necho hello")
	env.createButton("test", script)

	res := env.run("press", "test", "--json")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", res.ExitCode, res.Stderr)
	}

	resp := parseJSON(t, res.Stdout)
	if !resp.OK {
		t.Fatal("expected ok: true")
	}

	pr := parsePressResult(t, resp.Data)
	if pr.Status != "ok" {
		t.Errorf("status = %q, want ok", pr.Status)
	}
	if pr.ExitCode != 0 {
		t.Errorf("exit_code = %d, want 0", pr.ExitCode)
	}
	if !strings.Contains(pr.Stdout, "hello") {
		t.Errorf("stdout = %q, want to contain 'hello'", pr.Stdout)
	}
	if pr.Button != "test" {
		t.Errorf("button = %q, want 'test'", pr.Button)
	}
	if pr.DurationMs < 0 {
		t.Errorf("duration_ms = %d, want >= 0", pr.DurationMs)
	}
	if pr.StartedAt == "" {
		t.Error("started_at should be set")
	}
}

func TestPress_WithArgs(t *testing.T) {
	env := newTestEnv(t)
	script := env.createScriptWithContent("greet.sh", "#!/bin/sh\necho \"Hello, $BUTTONS_ARG_NAME!\"")
	env.createButton("greet", script, "--arg", "name:string:required")

	res := env.run("press", "greet", "--arg", "name=Bobak", "--json")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", res.ExitCode, res.Stderr)
	}

	resp := parseJSON(t, res.Stdout)
	pr := parsePressResult(t, resp.Data)
	if pr.Status != "ok" {
		t.Errorf("status = %q, want ok", pr.Status)
	}
	if !strings.Contains(pr.Stdout, "Hello, Bobak!") {
		t.Errorf("stdout = %q, want to contain 'Hello, Bobak!'", pr.Stdout)
	}
	if pr.Args["name"] != "Bobak" {
		t.Errorf("args[name] = %q, want 'Bobak'", pr.Args["name"])
	}
}

func TestPress_MissingRequiredArg(t *testing.T) {
	env := newTestEnv(t)
	script := env.createScript("test.sh")
	env.createButton("test", script, "--arg", "url:string:required")

	res := env.run("press", "test", "--json")
	if res.ExitCode == 0 {
		t.Fatal("expected non-zero exit")
	}

	resp := parseJSON(t, res.Stdout)
	if resp.OK {
		t.Fatal("expected ok: false")
	}
	if resp.Error.Code != "MISSING_ARG" {
		t.Errorf("code = %q, want MISSING_ARG", resp.Error.Code)
	}
	if !strings.Contains(resp.Error.Message, "url") {
		t.Errorf("message = %q, should mention 'url'", resp.Error.Message)
	}
}

func TestPress_NotFound(t *testing.T) {
	env := newTestEnv(t)

	res := env.run("press", "nonexistent", "--json")
	if res.ExitCode == 0 {
		t.Fatal("expected non-zero exit")
	}

	resp := parseJSON(t, res.Stdout)
	if resp.OK {
		t.Fatal("expected ok: false")
	}
	if resp.Error.Code != "NOT_FOUND" {
		t.Errorf("code = %q, want NOT_FOUND", resp.Error.Code)
	}
}

func TestPress_NonZeroExit(t *testing.T) {
	env := newTestEnv(t)
	script := env.createScriptWithContent("fail.sh", "#!/bin/sh\necho fail >&2\nexit 42")
	env.createButton("fail", script)

	// Script failure returns ok:false with error info
	res := env.run("press", "fail", "--json")
	if res.ExitCode == 0 {
		t.Fatal("expected non-zero exit for failed script")
	}

	resp := parseJSON(t, res.Stdout)
	if resp.OK {
		t.Fatal("expected ok: false for failed script")
	}
	if resp.Error.Code != "SCRIPT_ERROR" {
		t.Errorf("code = %q, want SCRIPT_ERROR", resp.Error.Code)
	}
}

func TestPress_Timeout(t *testing.T) {
	env := newTestEnv(t)
	script := env.createScriptWithContent("slow.sh", "#!/bin/sh\nsleep 30\necho done")
	env.createButton("slow", script)

	// Timeout returns ok:false with TIMEOUT error
	res := env.run("press", "slow", "--timeout", "1", "--json")
	if res.ExitCode == 0 {
		t.Fatal("expected non-zero exit for timeout")
	}

	resp := parseJSON(t, res.Stdout)
	if resp.OK {
		t.Fatal("expected ok: false for timeout")
	}
	if resp.Error.Code != "TIMEOUT" {
		t.Errorf("code = %q, want TIMEOUT", resp.Error.Code)
	}
}

func TestPress_DryRun(t *testing.T) {
	env := newTestEnv(t)
	script := env.createScriptWithContent("hello.sh", "#!/bin/sh\necho hello")
	env.createButton("test", script, "--arg", "name:string:optional")

	res := env.run("press", "test", "--dry-run", "--arg", "name=world", "--json")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", res.ExitCode, res.Stderr)
	}

	resp := parseJSON(t, res.Stdout)
	if !resp.OK {
		t.Fatal("expected ok: true")
	}

	var info map[string]json.RawMessage
	if err := json.Unmarshal(resp.Data, &info); err != nil {
		t.Fatalf("failed to parse dry-run data: %v", err)
	}

	// Should have button name, runtime, timeout, args, env
	for _, key := range []string{"button", "runtime", "timeout", "args", "env"} {
		if _, ok := info[key]; !ok {
			t.Errorf("dry-run missing key %q", key)
		}
	}
}

func TestPress_CodeFileDeleted(t *testing.T) {
	env := newTestEnv(t)
	script := env.createScriptWithContent("temp.sh", "#!/bin/sh\necho hi")
	env.createButton("temp", script)

	// Delete the code file from inside the button folder
	codePath := filepath.Join(env.home, "buttons", "temp", "main.sh")
	os.Remove(codePath)

	res := env.run("press", "temp", "--json")
	if res.ExitCode == 0 {
		t.Fatal("expected non-zero exit for missing code file")
	}

	resp := parseJSON(t, res.Stdout)
	if resp.OK {
		t.Fatal("expected ok: false for missing code file")
	}
	if resp.Error.Code != "SCRIPT_ERROR" {
		t.Errorf("code = %q, want SCRIPT_ERROR", resp.Error.Code)
	}
}

func TestPress_ArgWithEquals(t *testing.T) {
	env := newTestEnv(t)
	script := env.createScriptWithContent("echo-url.sh", "#!/bin/sh\necho $BUTTONS_ARG_URL")
	env.createButton("echo-url", script, "--arg", "url:string:required")

	// Value contains '=' — should split on first '=' only
	res := env.run("press", "echo-url", "--arg", "url=https://example.com?foo=bar", "--json")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", res.ExitCode, res.Stderr)
	}

	pr := parsePressResult(t, parseJSON(t, res.Stdout).Data)
	if !strings.Contains(pr.Stdout, "https://example.com?foo=bar") {
		t.Errorf("stdout = %q, want URL with query params", pr.Stdout)
	}
}
