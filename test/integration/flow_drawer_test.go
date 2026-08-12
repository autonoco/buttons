package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readFlowDrawer(t *testing.T, env *testEnv, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(env.home, "drawers", name, "drawer.json"))
	if err != nil {
		t.Fatalf("read drawer.json: %v", err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("decode drawer.json: %v", err)
	}
	return value
}

func TestFlowDrawerAuthoringThroughCLI(t *testing.T) {
	env := newTestEnv(t)

	r := env.run("drawer", "create", "software-delivery", "--kind", "flow", "--json")
	if r.ExitCode != 0 {
		t.Fatalf("create flow drawer: exit=%d stdout=%s stderr=%s", r.ExitCode, r.Stdout, r.Stderr)
	}
	created := readFlowDrawer(t, env, "software-delivery")
	if created["schema_version"] != float64(2) || created["drawer_kind"] != "flow" {
		t.Fatalf("created flow identity = %#v", created)
	}
	if _, mixed := created["steps"]; mixed {
		t.Fatalf("flow drawer must not scaffold steps: %#v", created["steps"])
	}

	commands := [][]string{
		{"drawer", "software-delivery", "stage", "add", "intake", "--title", "Intake"},
		{"drawer", "software-delivery", "stage", "add", "research", "--title", "Research"},
		{"drawer", "software-delivery", "set", "flow.initial_stage=intake"},
		{"drawer", "software-delivery", "set", "flow.manager.agent=activation.manager"},
		{"drawer", "software-delivery", "set", "flow.manager.heartbeat_seconds=300"},
		{"drawer", "software-delivery", "set", `flow.stages.intake.transitions=["research"]`},
		{"drawer", "software-delivery", "set", "flow.stages.research.worker.agent=activation.worker"},
	}
	for _, args := range commands {
		r = env.run(args...)
		if r.ExitCode != 0 {
			t.Fatalf("%s: exit=%d stdout=%s stderr=%s", strings.Join(args, " "), r.ExitCode, r.Stdout, r.Stderr)
		}
	}

	r = env.run("drawer", "software-delivery", "--json")
	if r.ExitCode != 0 {
		t.Fatalf("inspect flow drawer: exit=%d stdout=%s stderr=%s", r.ExitCode, r.Stdout, r.Stderr)
	}
	if !strings.Contains(r.Stdout, `"ok": true`) {
		t.Fatalf("flow validation not green: %s", r.Stdout)
	}
	for _, want := range []string{`"schema_version": 2`, `"version": "1"`, `"drawer_kind": "flow"`, `"initial_stage": "intake"`} {
		if !strings.Contains(r.Stdout, want) {
			t.Errorf("flow inspection missing %s: %s", want, r.Stdout)
		}
	}

	got := readFlowDrawer(t, env, "software-delivery")
	flow := got["flow"].(map[string]any)
	if flow["initial_stage"] != "intake" {
		t.Fatalf("initial_stage = %#v", flow["initial_stage"])
	}
	manager := flow["manager"].(map[string]any)
	if manager["agent"] != "activation.manager" || manager["heartbeat_seconds"] != float64(300) {
		t.Fatalf("manager = %#v", manager)
	}
	stages := flow["stages"].([]any)
	if len(stages) != 2 {
		t.Fatalf("stages = %#v", stages)
	}
	intake := stages[0].(map[string]any)
	if intake["id"] != "intake" || intake["title"] != "Intake" {
		t.Fatalf("intake = %#v", intake)
	}
	transitions := intake["transitions"].([]any)
	if len(transitions) != 1 || transitions[0] != "research" {
		t.Fatalf("intake transitions = %#v", transitions)
	}

	r = env.run("drawer", "software-delivery", "stage", "add", "intake", "--title", "Duplicate", "--json")
	if r.ExitCode == 0 || !strings.Contains(r.Stdout+r.Stderr, "already exists") {
		t.Fatalf("duplicate stage should fail: exit=%d stdout=%s stderr=%s", r.ExitCode, r.Stdout, r.Stderr)
	}
}

func TestActionDrawerCreateKindRemainsCompatible(t *testing.T) {
	env := newTestEnv(t)
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "implicit", args: []string{"drawer", "create", "implicit-action", "--json"}},
		{name: "explicit", args: []string{"drawer", "create", "explicit-action", "--kind", "action", "--json"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := env.run(tc.args...)
			if r.ExitCode != 0 {
				t.Fatalf("create action: exit=%d stdout=%s stderr=%s", r.ExitCode, r.Stdout, r.Stderr)
			}
			name := tc.args[2]
			got := readFlowDrawer(t, env, name)
			if got["drawer_kind"] != "action" {
				t.Fatalf("drawer_kind = %#v", got["drawer_kind"])
			}
			if _, ok := got["steps"]; !ok {
				t.Fatalf("action drawer did not scaffold steps: %#v", got)
			}
		})
	}
}

func TestFlowDrawerRejectsActionExecutionCommands(t *testing.T) {
	env := newTestEnv(t)
	r := env.run("drawer", "create", "managed-flow", "--kind", "flow", "--json")
	if r.ExitCode != 0 {
		t.Fatalf("create flow: %s", r.Stderr)
	}
	r = env.run("create", "build", "--runtime", "shell", "--code", "echo ok", "--json")
	if r.ExitCode != 0 {
		t.Fatalf("create button: %s", r.Stderr)
	}

	// Adding action steps to a flow drawer remains forbidden.
	r = env.run("drawer", "managed-flow", "add", "build", "--json")
	if r.ExitCode == 0 || !strings.Contains(r.Stdout+r.Stderr, "flow drawer") {
		t.Fatalf("add should reject action execution: exit=%d stdout=%s stderr=%s", r.ExitCode, r.Stdout, r.Stderr)
	}

	// Press is now allowed through the flow compiler; an incomplete
	// definition fails validation rather than FLOW_RUNTIME_REQUIRED.
	r = env.run("drawer", "managed-flow", "press", "--json")
	if r.ExitCode == 0 {
		t.Fatalf("press incomplete flow should fail validation: stdout=%s", r.Stdout)
	}
	out := r.Stdout + r.Stderr
	if strings.Contains(out, "FLOW_RUNTIME_REQUIRED") {
		t.Fatalf("press should not return FLOW_RUNTIME_REQUIRED anymore: %s", out)
	}
}

func TestFlowDrawerPressCompilesAndRuns(t *testing.T) {
	env := newTestEnv(t)

	// Install local pipeline buttons used by CompileFlow.
	r := env.run("flow", "ensure-buttons", "--on", "local", "--json")
	if r.ExitCode != 0 {
		t.Fatalf("ensure-buttons: exit=%d stdout=%s stderr=%s", r.ExitCode, r.Stdout, r.Stderr)
	}

	commands := [][]string{
		{"drawer", "create", "demo-board", "--kind", "flow", "--json"},
		{"drawer", "demo-board", "stage", "add", "intake", "--title", "Intake"},
		{"drawer", "demo-board", "stage", "add", "done", "--title", "Done"},
		{"drawer", "demo-board", "set", "flow.initial_stage=intake"},
		{"drawer", "demo-board", "set", "flow.manager.agent=activation.manager"},
		{"drawer", "demo-board", "set", `flow.stages.intake.transitions=["done"]`},
		{"drawer", "demo-board", "set", "flow.provider=local"},
	}
	for _, args := range commands {
		r = env.run(args...)
		if r.ExitCode != 0 {
			t.Fatalf("%s: exit=%d stdout=%s stderr=%s", strings.Join(args, " "), r.ExitCode, r.Stdout, r.Stderr)
		}
	}

	r = env.run("flow", "task", "add", "demo-board", "login page 500s on Safari", "--json")
	if r.ExitCode != 0 {
		t.Fatalf("task add: exit=%d stdout=%s stderr=%s", r.ExitCode, r.Stdout, r.Stderr)
	}

	r = env.run("drawer", "demo-board", "press", "--json")
	if r.ExitCode != 0 {
		t.Fatalf("press flow: exit=%d stdout=%s stderr=%s", r.ExitCode, r.Stdout, r.Stderr)
	}
	var pressResult map[string]any
	if err := json.Unmarshal([]byte(r.Stdout), &pressResult); err != nil {
		t.Fatalf("parse press output: %v %s", err, r.Stdout)
	}
	if data, ok := pressResult["data"].(map[string]any); ok {
		if data["status"] != "ok" {
			t.Fatalf("expected data.status=ok, got %v: %s", data["status"], r.Stdout)
		}
	} else if pressResult["status"] != "ok" {
		t.Fatalf("expected status=ok, got %v: %s", pressResult["status"], r.Stdout)
	}
}
