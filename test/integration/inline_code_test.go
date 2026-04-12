package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreate_InlineCode(t *testing.T) {
	env := newTestEnv(t)

	res := env.run("create", "test", "--code", "echo hello", "--json")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", res.ExitCode, res.Stderr)
	}

	resp := parseJSON(t, res.Stdout)
	btn := parseButton(t, resp.Data)
	if btn.Runtime != "shell" {
		t.Errorf("runtime = %q, want shell", btn.Runtime)
	}

	// Code file should exist in button folder
	codePath := filepath.Join(env.home, "buttons", "test", "main.sh")
	if _, err := os.Stat(codePath); err != nil {
		t.Fatalf("code file not found: %v", err)
	}
}

func TestCreate_InlineCode_WithRuntime(t *testing.T) {
	env := newTestEnv(t)

	res := env.run("create", "test", "--runtime", "python", "--code", "print('hi')", "--json")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", res.ExitCode, res.Stderr)
	}

	btn := parseButton(t, parseJSON(t, res.Stdout).Data)
	if btn.Runtime != "python" {
		t.Errorf("runtime = %q, want python", btn.Runtime)
	}

	// Should create main.py, not main.sh
	codePath := filepath.Join(env.home, "buttons", "test", "main.py")
	if _, err := os.Stat(codePath); err != nil {
		t.Fatalf("python code file not found: %v", err)
	}
}

func TestCreate_MutualExclusion_FileAndCode(t *testing.T) {
	env := newTestEnv(t)
	script := env.createScript("test.sh")

	res := env.run("create", "test", "--file", script, "--code", "echo hi", "--json")
	if res.ExitCode == 0 {
		t.Fatal("expected non-zero exit")
	}
	resp := parseJSON(t, res.Stdout)
	if resp.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", resp.Error.Code)
	}
}

func TestCreate_Scaffold_NoSource(t *testing.T) {
	env := newTestEnv(t)

	// With no source flag, create scaffolds a shell button with a placeholder main.sh.
	res := env.run("create", "test", "--json")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", res.ExitCode, res.Stderr)
	}

	btn := parseButton(t, parseJSON(t, res.Stdout).Data)
	if btn.Runtime != "shell" {
		t.Errorf("runtime = %q, want shell", btn.Runtime)
	}

	// main.sh should exist with the shebang placeholder so the agent can open it.
	codePath := filepath.Join(env.home, "buttons", "test", "main.sh")
	data, err := os.ReadFile(codePath)
	if err != nil {
		t.Fatalf("scaffolded main.sh not found: %v", err)
	}
	if !strings.HasPrefix(string(data), "#!/bin/sh") {
		t.Errorf("main.sh should start with shebang, got: %q", string(data[:min(len(data), 40)]))
	}
}

func TestCreate_RuntimeWithFile(t *testing.T) {
	env := newTestEnv(t)
	script := env.createScript("test.sh")

	res := env.run("create", "test", "--file", script, "--runtime", "python", "--json")
	if res.ExitCode == 0 {
		t.Fatal("expected non-zero exit for --runtime with --file")
	}
	resp := parseJSON(t, res.Stdout)
	if resp.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", resp.Error.Code)
	}
}

func TestPress_InlineCode_Executes(t *testing.T) {
	env := newTestEnv(t)
	env.run("create", "greet", "--code", `echo "Hello, $BUTTONS_ARG_NAME!"`, "--arg", "name:string:required", "--json")

	res := env.run("press", "greet", "--arg", "name=World", "--json")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", res.ExitCode, res.Stderr)
	}

	pr := parsePressResult(t, parseJSON(t, res.Stdout).Data)
	if pr.Status != "ok" {
		t.Errorf("status = %q, want ok", pr.Status)
	}
	if !strings.Contains(pr.Stdout, "Hello, World!") {
		t.Errorf("stdout = %q, want to contain 'Hello, World!'", pr.Stdout)
	}
}

func TestPress_InlineCode_Timeout(t *testing.T) {
	env := newTestEnv(t)
	env.run("create", "slow", "--code", "sleep 30", "--json")

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

func TestPress_InlineCode_CodeFileExists(t *testing.T) {
	env := newTestEnv(t)
	env.run("create", "showpath", "--code", "echo $0", "--json")

	res := env.run("press", "showpath", "--json")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d", res.ExitCode)
	}

	pr := parsePressResult(t, parseJSON(t, res.Stdout).Data)
	codePath := strings.TrimSpace(pr.Stdout)
	// Code file should still exist (it's permanent, not a temp file)
	if _, err := os.Stat(codePath); err != nil {
		t.Errorf("code file %s should exist (permanent, not temp)", codePath)
	}
}

func TestCreate_Scaffold_Python(t *testing.T) {
	env := newTestEnv(t)

	// With --runtime python and no source, create scaffolds main.py.
	res := env.run("create", "test", "--runtime", "python", "--json")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", res.ExitCode, res.Stderr)
	}

	btn := parseButton(t, parseJSON(t, res.Stdout).Data)
	if btn.Runtime != "python" {
		t.Errorf("runtime = %q, want python", btn.Runtime)
	}

	codePath := filepath.Join(env.home, "buttons", "test", "main.py")
	data, err := os.ReadFile(codePath)
	if err != nil {
		t.Fatalf("scaffolded main.py not found: %v", err)
	}
	if !strings.HasPrefix(string(data), "#!/usr/bin/env python3") {
		t.Errorf("main.py should start with python shebang, got: %q", string(data[:min(len(data), 40)]))
	}
}

func TestPress_InlineCode_RuntimeMissing(t *testing.T) {
	env := newTestEnv(t)
	env.run("create", "test", "--runtime", "fakeLang999", "--code", "print('hi')", "--json")

	res := env.run("press", "test", "--json")
	if res.ExitCode == 0 {
		t.Fatal("expected non-zero exit for missing runtime")
	}

	resp := parseJSON(t, res.Stdout)
	if resp.OK {
		t.Fatal("expected ok: false for missing runtime")
	}
	if resp.Error.Code != "RUNTIME_MISSING" {
		t.Errorf("code = %q, want RUNTIME_MISSING", resp.Error.Code)
	}
}

func TestPress_InlineCode_FailureStillRecorded(t *testing.T) {
	env := newTestEnv(t)
	env.run("create", "failtest", "--code", "echo failing; exit 1", "--json")

	res := env.run("press", "failtest", "--json")
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

	// Failure should still be recorded in history
	res = env.run("history", "failtest", "--json")
	runs := parseHistoryRuns(t, parseJSON(t, res.Stdout).Data)
	if len(runs) != 1 {
		t.Fatalf("expected 1 run in history after failure, got %d", len(runs))
	}
}

func TestCreate_InlineCode_ExceedsMaxSize(t *testing.T) {
	env := newTestEnv(t)

	bigCode := strings.Repeat("echo hi\n", 9000) // ~72KB
	res := env.run("create", "test", "--code", bigCode, "--json")
	if res.ExitCode == 0 {
		t.Fatal("expected non-zero exit for code > 64KB")
	}
	resp := parseJSON(t, res.Stdout)
	if resp.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", resp.Error.Code)
	}
}

func TestCreate_ButtonFolderStructure(t *testing.T) {
	env := newTestEnv(t)

	env.run("create", "mybutton", "--code", "echo hello", "-d", "test button", "--json")

	btnDir := filepath.Join(env.home, "buttons", "mybutton")

	// Check all expected files exist
	for _, f := range []string{"button.json", "main.sh", "AGENT.md"} {
		if _, err := os.Stat(filepath.Join(btnDir, f)); err != nil {
			t.Errorf("expected %s to exist: %v", f, err)
		}
	}

	// Check pressed/ directory exists
	if info, err := os.Stat(filepath.Join(btnDir, "pressed")); err != nil || !info.IsDir() {
		t.Error("expected pressed/ directory to exist")
	}
}
