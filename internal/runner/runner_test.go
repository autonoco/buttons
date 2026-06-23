package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/autonoco/buttons/internal/button"
)

func writeButton(t *testing.T, home, name, spec, body string) {
	t.Helper()
	dir := filepath.Join(home, "buttons", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "button.json"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.sh"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestPressRunsAndRecords(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	writeButton(t, home, "echo",
		`{"schema_version":1,"name":"echo","runtime":"shell","env":{},"timeout_seconds":30,"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`,
		"#!/bin/sh\necho ok\n")

	res, err := Press(context.Background(), "echo", nil, Options{RecordHistory: true})
	if err != nil {
		t.Fatalf("press: %v", err)
	}
	if res.Status != "ok" || res.Stdout != "ok\n" {
		t.Fatalf("unexpected result: status=%q stdout=%q", res.Status, res.Stdout)
	}
}

func TestPressRejectsMissingRequiredArg(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	writeButton(t, home, "need",
		`{"schema_version":1,"name":"need","runtime":"shell","env":{},"timeout_seconds":30,"args":[{"name":"x","type":"string","required":true}],"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`,
		"#!/bin/sh\necho hi\n")

	_, err := Press(context.Background(), "need", nil, Options{})
	if err == nil {
		t.Fatal("expected error for missing required arg")
	}
	var se *button.ServiceError
	if !asServiceError(err, &se) || se.Code != "MISSING_ARG" {
		t.Fatalf("want MISSING_ARG ServiceError, got %v", err)
	}
}

func TestPressMissingButton(t *testing.T) {
	t.Setenv("BUTTONS_HOME", t.TempDir())
	if _, err := Press(context.Background(), "ghost", nil, Options{}); err == nil {
		t.Fatal("expected error pressing a missing button")
	}
}

func TestMaxTimeoutClamp(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	// Button asks for 300s but MCP-style cap is 1s; a 5s sleep must time out.
	writeButton(t, home, "slow",
		`{"schema_version":1,"name":"slow","runtime":"shell","env":{},"timeout_seconds":300,"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`,
		"#!/bin/sh\nsleep 5\n")

	res, err := Press(context.Background(), "slow", nil, Options{MaxTimeoutSeconds: 1})
	if err != nil {
		t.Fatalf("press: %v", err)
	}
	if res.Status != "timeout" {
		t.Fatalf("expected timeout under the 1s cap, got %q", res.Status)
	}
}

// asServiceError is a tiny errors.As shim to avoid importing errors twice.
func asServiceError(err error, target **button.ServiceError) bool {
	if se, ok := err.(*button.ServiceError); ok {
		*target = se
		return true
	}
	return false
}
