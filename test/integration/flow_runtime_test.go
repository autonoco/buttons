package integration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestButtonsFlowResearchDeckAcceptance(t *testing.T) {
	env := newTestEnv(t)

	r := env.run("add", "@buttonsflow/research-deck", "--json")
	if r.ExitCode != 0 {
		t.Fatalf("add research-deck: exit=%d stdout=%s stderr=%s", r.ExitCode, r.Stdout, r.Stderr)
	}

	r = env.run("flow", "init", "research-deck", "--on", "local", "--json")
	if r.ExitCode != 0 {
		t.Fatalf("flow init: exit=%d stdout=%s stderr=%s", r.ExitCode, r.Stdout, r.Stderr)
	}

	r = env.run("flow", "task", "add", "research-deck", "Q3 competitive landscape for leadership offsite", "--json")
	if r.ExitCode != 0 {
		t.Fatalf("task add: exit=%d stdout=%s stderr=%s", r.ExitCode, r.Stdout, r.Stderr)
	}

	// Deterministic local perform advances one stage per press. Review is gated.
	done := false
	for i := 0; i < 12; i++ {
		r = env.run("drawer", "research-deck", "press", "--json")
		if r.ExitCode != 0 {
			t.Fatalf("press %d: exit=%d stdout=%s stderr=%s", i, r.ExitCode, r.Stdout, r.Stderr)
		}
		approvePendingTasks(t, env, "research-deck")
		if taskCountWithStatus(t, env, "research-deck", "done") > 0 {
			done = true
			break
		}
	}
	if !done {
		t.Fatal("expected at least one task to reach status=done")
	}

	r = env.run("flow", "task", "list", "research-deck", "--filter", "status=done", "--json")
	if r.ExitCode != 0 {
		t.Fatalf("task list: exit=%d stdout=%s stderr=%s", r.ExitCode, r.Stdout, r.Stderr)
	}
	if !strings.Contains(r.Stdout, "done") {
		t.Fatalf("expected done tasks in output: %s", r.Stdout)
	}
}

func approvePendingTasks(t *testing.T, env *testEnv, board string) {
	t.Helper()
	tasksDir := filepath.Join(env.home, "flows", board, "tasks")
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(tasksDir, e.Name()))
		if err != nil {
			continue
		}
		var task map[string]any
		if json.Unmarshal(data, &task) != nil {
			continue
		}
		props, _ := task["props"].(map[string]any)
		if props == nil {
			continue
		}
		if _, ok := props["flow.pending_approval"]; !ok {
			continue
		}
		id, _ := task["id"].(string)
		if id == "" {
			id = strings.TrimSuffix(e.Name(), ".json")
		}
		r := env.run("flow", "approve", board, id, "--json")
		if r.ExitCode != 0 {
			t.Fatalf("approve %s: exit=%d stdout=%s stderr=%s", id, r.ExitCode, r.Stdout, r.Stderr)
		}
	}
}

func taskCountWithStatus(t *testing.T, env *testEnv, board, status string) int {
	t.Helper()
	r := env.run("flow", "task", "list", board, "--filter", "status="+status, "--json")
	if r.ExitCode != 0 {
		t.Fatalf("list: %s", r.Stderr)
	}
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Stdout string `json:"stdout"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &envelope); err != nil {
		t.Fatalf("parse list envelope: %v %s", err, r.Stdout)
	}
	payload := envelope.Data.Stdout
	if payload == "" {
		payload = r.Stdout
	}
	var listed struct {
		Items []map[string]any `json:"items"`
		Count int              `json:"count"`
	}
	if err := json.Unmarshal([]byte(payload), &listed); err != nil {
		t.Fatalf("parse list payload: %v %s", err, payload)
	}
	count := 0
	for _, item := range listed.Items {
		if fmt.Sprint(item["status"]) == status {
			count++
		}
	}
	return count
}

func TestFlowInitGitHubWritesActionsWorkflow(t *testing.T) {
	env := newTestEnv(t)
	r := env.run("flow", "ensure-buttons", "--on", "github", "--json")
	if r.ExitCode != 0 {
		t.Fatalf("ensure github buttons: %s", r.Stderr)
	}
	commands := [][]string{
		{"drawer", "create", "gh-board", "--kind", "flow", "--json"},
		{"drawer", "gh-board", "stage", "add", "intake", "--title", "Intake"},
		{"drawer", "gh-board", "stage", "add", "done", "--title", "Done"},
		{"drawer", "gh-board", "set", "flow.initial_stage=intake"},
		{"drawer", "gh-board", "set", "flow.manager.agent=activation.manager"},
		{"drawer", "gh-board", "set", `flow.stages.intake.transitions=["done"]`},
	}
	for _, args := range commands {
		r = env.run(args...)
		if r.ExitCode != 0 {
			t.Fatalf("%v: %s", args, r.Stderr)
		}
	}
	r = env.run("flow", "init", "gh-board", "--on", "github", "--no-verify", "--json")
	if r.ExitCode != 0 {
		t.Fatalf("init github: exit=%d stdout=%s stderr=%s", r.ExitCode, r.Stdout, r.Stderr)
	}
	wf := filepath.Join(env.home, "flows", "gh-board", "github-actions", "press.yml")
	data, err := os.ReadFile(wf)
	if err != nil {
		t.Fatalf("actions workflow missing: %v", err)
	}
	if !strings.Contains(string(data), "schedule:") || !strings.Contains(string(data), "drawer gh-board press") {
		t.Fatalf("unexpected workflow: %s", data)
	}
}
