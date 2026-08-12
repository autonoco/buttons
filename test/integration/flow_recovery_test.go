package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFlowKillMidTurnRecoversViaStaleness(t *testing.T) {
	env := newTestEnv(t)
	r := env.run("flow", "ensure-buttons", "--on", "local", "--json")
	if r.ExitCode != 0 {
		t.Fatalf("ensure-buttons: %s", r.Stderr)
	}
	for _, args := range [][]string{
		{"drawer", "create", "recover", "--kind", "flow", "--json"},
		{"drawer", "recover", "stage", "add", "intake", "--title", "Intake"},
		{"drawer", "recover", "stage", "add", "done", "--title", "Done"},
		{"drawer", "recover", "set", "flow.initial_stage=intake"},
		{"drawer", "recover", "set", "flow.manager.agent=activation.manager"},
		{"drawer", "recover", "set", `flow.stages.intake.transitions=["done"]`},
		{"drawer", "recover", "set", "flow.stages.intake.timeout_seconds=1"},
		{"drawer", "recover", "set", "flow.provider=local"},
	} {
		r = env.run(args...)
		if r.ExitCode != 0 {
			t.Fatalf("%v: %s", args, r.Stderr)
		}
	}
	r = env.run("flow", "task", "add", "recover", "orphan-me", "--json")
	if r.ExitCode != 0 {
		t.Fatalf("task add: %s", r.Stderr)
	}

	// Simulate a crashed claim: write claimed_by without finishing apply.
	tasksDir := filepath.Join(env.home, "flows", "recover", "tasks")
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		t.Fatal(err)
	}
	var taskPath string
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			taskPath = filepath.Join(tasksDir, e.Name())
			break
		}
	}
	if taskPath == "" {
		t.Fatal("no task file")
	}
	data, err := os.ReadFile(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	var task map[string]any
	if err := json.Unmarshal(data, &task); err != nil {
		t.Fatal(err)
	}
	props, _ := task["props"].(map[string]any)
	if props == nil {
		props = map[string]any{}
		task["props"] = props
	}
	// Ensure status has not advanced (still intake).
	props["status"] = "intake"
	task["status"] = "intake"
	props["flow.claimed_by"] = "crashed-agent"
	props["flow.claimed_at"] = time.Now().UTC().Add(-3 * time.Second).Format(time.RFC3339)
	task["timeout_seconds"] = 1
	out, _ := json.MarshalIndent(task, "", "  ")
	if err := os.WriteFile(taskPath, append(out, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	// Next press should reclaim via staleness and advance to done; status never regresses.
	r = env.run("drawer", "recover", "press", "--json")
	if r.ExitCode != 0 {
		t.Fatalf("recover press: exit=%d stdout=%s stderr=%s", r.ExitCode, r.Stdout, r.Stderr)
	}
	data, err = os.ReadFile(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &task); err != nil {
		t.Fatal(err)
	}
	props, _ = task["props"].(map[string]any)
	status := ""
	if props != nil {
		status, _ = props["status"].(string)
	}
	if status == "" {
		status, _ = task["status"].(string)
	}
	if status != "done" {
		t.Fatalf("expected status=done after reclaim press, got %#v task=%s", status, data)
	}
}
