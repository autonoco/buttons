package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var (
	binaryPath string
	buildDir   string
)

func projectRoot() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(f), "..", "..")
}

func TestMain(m *testing.M) {
	var err error
	buildDir, err = os.MkdirTemp("", "buttons-test-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create build dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(buildDir)

	binaryPath = filepath.Join(buildDir, "buttons")
	cmd := exec.Command("go", "build", "-o", binaryPath, ".")
	cmd.Dir = projectRoot()
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "build failed: %v\n%s\n", err, out)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

func ensureBinary(t *testing.T) string {
	t.Helper()
	if binaryPath == "" {
		t.Fatal("binary not built — TestMain should have built it")
	}
	return binaryPath
}

type testEnv struct {
	t      *testing.T
	home   string
	binary string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	binary := ensureBinary(t)
	home := t.TempDir()
	return &testEnv{t: t, home: home, binary: binary}
}

func (e *testEnv) createScript(name string) string {
	e.t.Helper()
	path := filepath.Join(e.home, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho hello"), 0755); err != nil {
		e.t.Fatalf("failed to create script: %v", err)
	}
	return path
}

type result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

func (e *testEnv) run(args ...string) result {
	e.t.Helper()
	cmd := exec.Command(e.binary, args...)
	cmd.Env = append(os.Environ(), "BUTTONS_HOME="+e.home)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			e.t.Fatalf("failed to run command: %v", err)
		}
	}

	return result{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}
}

func (e *testEnv) runWithStdin(stdinContent string, args ...string) result {
	e.t.Helper()
	cmd := exec.Command(e.binary, args...)
	cmd.Env = append(os.Environ(), "BUTTONS_HOME="+e.home)
	cmd.Stdin = strings.NewReader(stdinContent)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			e.t.Fatalf("failed to run command: %v", err)
		}
	}

	return result{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}
}

type jsonResponse struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error *jsonError      `json:"error,omitempty"`
}

type jsonError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func parseJSON(t *testing.T, stdout string) jsonResponse {
	t.Helper()
	var resp jsonResponse
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
		t.Fatalf("failed to parse JSON response: %v\nraw: %s", err, stdout)
	}
	return resp
}

type buttonJSON struct {
	SchemaVersion  int               `json:"schema_version"`
	Name           string            `json:"name"`
	Description    string            `json:"description,omitempty"`
	Runtime        string            `json:"runtime"`
	File           string            `json:"file,omitempty"`
	Args           []argJSON         `json:"args,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	TimeoutSeconds int               `json:"timeout_seconds"`
	MCPEnabled     bool              `json:"mcp_enabled"`
	CreatedAt      string            `json:"created_at"`
	UpdatedAt      string            `json:"updated_at"`
}

type argJSON struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

func parseButton(t *testing.T, data json.RawMessage) buttonJSON {
	t.Helper()
	var btn buttonJSON
	if err := json.Unmarshal(data, &btn); err != nil {
		t.Fatalf("failed to parse button: %v", err)
	}
	return btn
}

func parseButtonList(t *testing.T, data json.RawMessage) []buttonJSON {
	t.Helper()
	var buttons []buttonJSON
	if err := json.Unmarshal(data, &buttons); err != nil {
		t.Fatalf("failed to parse button list: %v", err)
	}
	return buttons
}
