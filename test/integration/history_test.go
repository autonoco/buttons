package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type historyRun struct {
	ButtonName string            `json:"button_name"`
	StartedAt  string            `json:"started_at"`
	FinishedAt string            `json:"finished_at"`
	ExitCode   int               `json:"exit_code"`
	Status     string            `json:"status"`
	ErrorType  string            `json:"error_type,omitempty"`
	Stdout     string            `json:"stdout"`
	Stderr     string            `json:"stderr"`
	DurationMs int64             `json:"duration_ms"`
	Args       map[string]string `json:"args,omitempty"`
}

func parseHistoryRuns(t *testing.T, data json.RawMessage) []historyRun {
	t.Helper()
	var runs []historyRun
	if err := json.Unmarshal(data, &runs); err != nil {
		t.Fatalf("failed to parse history runs: %v\nraw: %s", err, data)
	}
	return runs
}

func TestHistory_PressWritesHistory(t *testing.T) {
	env := newTestEnv(t)
	script := env.createScriptWithContent("hello.sh", "#!/bin/sh\necho hello")
	env.createButton("test", script)

	res := env.run("press", "test", "--json")
	if res.ExitCode != 0 {
		t.Fatalf("press failed: %s", res.Stderr)
	}

	res = env.run("history", "test", "--json")
	if res.ExitCode != 0 {
		t.Fatalf("history failed: %s", res.Stderr)
	}

	resp := parseJSON(t, res.Stdout)
	runs := parseHistoryRuns(t, resp.Data)
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}

	r := runs[0]
	if r.ButtonName != "test" {
		t.Errorf("button_name = %q, want 'test'", r.ButtonName)
	}
	if r.Status != "ok" {
		t.Errorf("status = %q, want 'ok'", r.Status)
	}
	if !strings.Contains(r.Stdout, "hello") {
		t.Errorf("stdout = %q, want to contain 'hello'", r.Stdout)
	}
	if r.StartedAt == "" {
		t.Error("started_at should be set")
	}

	// Verify the run JSON landed in pressed/. The progress JSONL file
	// is co-located (one-per-press) so we filter by suffix here.
	pressedDir := filepath.Join(env.home, "buttons", "test", "pressed")
	entries, _ := os.ReadDir(pressedDir)
	jsonFiles := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") && !strings.HasSuffix(e.Name(), ".progress.jsonl") {
			jsonFiles++
		}
	}
	if jsonFiles != 1 {
		t.Fatalf("expected 1 history .json file, got %d (entries: %v)", jsonFiles, entries)
	}
}

func TestHistory_ErrorRecorded(t *testing.T) {
	env := newTestEnv(t)
	script := env.createScriptWithContent("fail.sh", "#!/bin/sh\nexit 1")
	env.createButton("fail", script)

	env.run("press", "fail", "--json")

	res := env.run("history", "fail", "--json")
	runs := parseHistoryRuns(t, parseJSON(t, res.Stdout).Data)

	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	if runs[0].Status != "error" {
		t.Errorf("status = %q, want 'error'", runs[0].Status)
	}
	if runs[0].ErrorType != "SCRIPT_ERROR" {
		t.Errorf("error_type = %q, want 'SCRIPT_ERROR'", runs[0].ErrorType)
	}
}

func TestHistory_TimeoutRecorded(t *testing.T) {
	env := newTestEnv(t)
	script := env.createScriptWithContent("slow.sh", "#!/bin/sh\nsleep 30")
	env.createButton("slow", script)

	env.run("press", "slow", "--timeout", "1", "--json")

	res := env.run("history", "slow", "--json")
	runs := parseHistoryRuns(t, parseJSON(t, res.Stdout).Data)

	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	if runs[0].Status != "timeout" {
		t.Errorf("status = %q, want 'timeout'", runs[0].Status)
	}
}

func TestHistory_DryRunExcluded(t *testing.T) {
	env := newTestEnv(t)
	script := env.createScriptWithContent("hello.sh", "#!/bin/sh\necho hello")
	env.createButton("test", script)

	env.run("press", "test", "--dry-run", "--json")

	res := env.run("history", "test", "--json")
	runs := parseHistoryRuns(t, parseJSON(t, res.Stdout).Data)

	if len(runs) != 0 {
		t.Fatalf("expected 0 runs after dry-run, got %d", len(runs))
	}
}

func TestHistory_FilterByButton(t *testing.T) {
	env := newTestEnv(t)
	script := env.createScriptWithContent("hello.sh", "#!/bin/sh\necho hello")
	env.createButton("alpha", script)
	env.createButton("beta", script)

	env.run("press", "alpha", "--json")
	env.run("press", "beta", "--json")

	res := env.run("history", "alpha", "--json")
	runs := parseHistoryRuns(t, parseJSON(t, res.Stdout).Data)

	if len(runs) != 1 {
		t.Fatalf("expected 1 run for alpha, got %d", len(runs))
	}
	if runs[0].ButtonName != "alpha" {
		t.Errorf("button_name = %q, want 'alpha'", runs[0].ButtonName)
	}
}

func TestHistory_AllButtons(t *testing.T) {
	env := newTestEnv(t)
	script := env.createScriptWithContent("hello.sh", "#!/bin/sh\necho hello")
	env.createButton("alpha", script)
	env.createButton("beta", script)

	env.run("press", "alpha", "--json")
	env.run("press", "beta", "--json")

	res := env.run("history", "--json")
	runs := parseHistoryRuns(t, parseJSON(t, res.Stdout).Data)

	if len(runs) != 2 {
		t.Fatalf("expected 2 total runs, got %d", len(runs))
	}
}

func TestHistory_EmptyHistory(t *testing.T) {
	env := newTestEnv(t)

	res := env.run("history", "--json")
	resp := parseJSON(t, res.Stdout)

	if !resp.OK {
		t.Fatal("expected ok: true")
	}

	if string(resp.Data) != "[]" {
		t.Fatalf("expected empty array [], got %s", resp.Data)
	}
}
