package drawer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeShellButton drops a runnable shell button into BUTTONS_HOME. argsJSON is
// the button.json "args" array (or "" for none).
func writeShellButton(t *testing.T, home, name, body, argsJSON string) {
	t.Helper()
	dir := filepath.Join(home, "buttons", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	args := ""
	if argsJSON != "" {
		args = `,"args":` + argsJSON
	}
	spec := `{"schema_version":1,"name":"` + name + `","runtime":"shell","env":{},"timeout_seconds":30` + args +
		`,"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, "button.json"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.sh"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestStepDeps(t *testing.T) {
	ids := map[string]bool{"a": true, "fetch-data": true, "b": true}
	deps := stepDeps(Step{
		ID: "b",
		Args: map[string]any{
			"x":   "${a.output.v}",
			"y":   "${ fetch-data.output.id ?? a.output.v }",
			"lit": "plain",
			"env": "${inputs.who}",
		},
	}, ids)
	if !deps["a"] || !deps["fetch-data"] {
		t.Fatalf("expected deps {a, fetch-data}, got %v", deps)
	}
	if deps["b"] {
		t.Fatal("a step should not depend on itself")
	}
	if len(deps) != 2 {
		t.Fatalf("inputs ref must not count as a dep: %v", deps)
	}
}

func TestExecuteParallelIndependent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	writeShellButton(t, home, "one", "#!/bin/sh\necho one\n", "")
	writeShellButton(t, home, "two", "#!/bin/sh\necho two\n", "")

	d := &Drawer{Name: "indep", Steps: []Step{
		{ID: "a", Kind: "button", Button: "one"},
		{ID: "b", Kind: "button", Button: "two"},
	}}
	res, err := NewExecutor().ExecuteParallel(context.Background(), d, map[string]any{}, ParallelOptions{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.Status != "ok" || len(res.Steps) != 2 {
		t.Fatalf("want ok with 2 steps, got status=%s steps=%d", res.Status, len(res.Steps))
	}
}

func TestExecuteParallelRespectsDependency(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	writeShellButton(t, home, "emit", "#!/bin/sh\necho '{\"v\":\"42\"}'\n", "")
	writeShellButton(t, home, "echoarg", "#!/bin/sh\necho \"$BUTTONS_ARG_X\"\n",
		`[{"name":"x","type":"string","required":true}]`)

	d := &Drawer{Name: "dep", Steps: []Step{
		{ID: "b", Kind: "button", Button: "echoarg", Args: map[string]any{"x": "${a.output.v}"}},
		{ID: "a", Kind: "button", Button: "emit"},
	}}
	res, err := NewExecutor().ExecuteParallel(context.Background(), d, map[string]any{}, ParallelOptions{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.Status != "ok" {
		t.Fatalf("want ok, got %s (%+v)", res.Status, res.Error)
	}
	// b ran after a and resolved a's output → "42".
	var bRun StepRun
	for _, s := range res.Steps {
		if s.ID == "b" {
			bRun = s
		}
	}
	if strings.TrimSpace(bRun.Stdout) != "42" {
		t.Fatalf("dependent step did not see upstream output: stdout=%q", bRun.Stdout)
	}
}

func TestExecuteParallelOnFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	writeShellButton(t, home, "ok1", "#!/bin/sh\necho ok\n", "")
	writeShellButton(t, home, "boom", "#!/bin/sh\nexit 3\n", "")

	mk := func() *Drawer {
		return &Drawer{Name: "fail", Steps: []Step{
			{ID: "a", Kind: "button", Button: "ok1"},
			{ID: "b", Kind: "button", Button: "boom"},
		}}
	}

	// continue: both steps run, overall failed.
	res, _ := NewExecutor().ExecuteParallel(context.Background(), mk(), map[string]any{}, ParallelOptions{OnFailure: "continue"})
	if res.Status != "failed" || len(res.Steps) != 2 {
		t.Fatalf("continue: want failed with 2 steps, got status=%s steps=%d", res.Status, len(res.Steps))
	}

	// stop: overall failed, failed step recorded.
	res, _ = NewExecutor().ExecuteParallel(context.Background(), mk(), map[string]any{}, ParallelOptions{OnFailure: "stop"})
	if res.Status != "failed" || res.FailedStep == "" {
		t.Fatalf("stop: want failed with a failed_step, got status=%s failed=%q", res.Status, res.FailedStep)
	}
}

func TestExecuteParallelDeadlock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	writeShellButton(t, home, "noop", "#!/bin/sh\necho hi\n", `[{"name":"x","type":"string","required":false}]`)

	// a ↔ b cyclic reference.
	d := &Drawer{Name: "cycle", Steps: []Step{
		{ID: "a", Kind: "button", Button: "noop", Args: map[string]any{"x": "${b.output.v}"}},
		{ID: "b", Kind: "button", Button: "noop", Args: map[string]any{"x": "${a.output.v}"}},
	}}
	res, err := NewExecutor().ExecuteParallel(context.Background(), d, map[string]any{}, ParallelOptions{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.Status != "failed" || res.Error == nil || res.Error.Code != "PARALLEL_DEADLOCK" {
		t.Fatalf("want PARALLEL_DEADLOCK, got status=%s err=%+v", res.Status, res.Error)
	}
}
