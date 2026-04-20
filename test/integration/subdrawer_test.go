package integration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSubDrawer_EndToEnd verifies that a parent drawer can call a
// child drawer, the child's Return block produces an output map,
// and that output flows into the parent's next step via
// ${<sub_step_id>.output.<field>} references.
//
// Topology:
//
//	parent-flow:
//	  build   → kind=drawer, drawer=child-build
//	  notify  → kind=button, args.msg = "built ${build.output.version}"
//
//	child-build (declares output_schema + return):
//	  emit → echo {"version":"1.2.3"}
//	  return: { version: ${emit.output.version} }
func TestSubDrawer_EndToEnd(t *testing.T) {
	env := newTestEnv(t)

	// Buttons.
	env.run("create", "emit-version",
		"--runtime", "shell",
		"--code", `echo '{"version":"1.2.3"}'`,
		"--json",
	)
	env.run("create", "echo-msg",
		"--runtime", "shell",
		"--code", `echo "{\"got\":\"$BUTTONS_ARG_MSG\"}"`,
		"--arg", "msg:string:required",
		"--json",
	)

	// Child drawer: emits a version, returns it through its
	// Return block.
	env.run("drawer", "create", "child-build", "--json")
	env.run("drawer", "child-build", "add", "emit-version", "--json")
	// Patch the child to add a Return block + output_schema.
	// Patching via direct file-write exercises the round-trip since
	// the CLI doesn't yet have a first-class `return` verb.
	childPath := filepath.Join(env.home, "drawers", "child-build", "drawer.json")
	data, err := os.ReadFile(childPath) // #nosec G304 — test scope
	if err != nil {
		t.Fatalf("read child: %v", err)
	}
	var child map[string]any
	if err := json.Unmarshal(data, &child); err != nil {
		t.Fatalf("parse child: %v", err)
	}
	child["return"] = map[string]any{
		"version": "${emit-version.output.version}",
	}
	child["output_schema"] = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"version": map[string]any{"type": "string"},
		},
	}
	out, _ := json.MarshalIndent(child, "", "  ")
	if err := os.WriteFile(childPath, out, 0o600); err != nil {
		t.Fatalf("write child: %v", err)
	}

	// Parent drawer: calls the child, then echoes its version.
	env.run("drawer", "create", "parent-flow", "--json")
	env.run("drawer", "parent-flow", "add", "drawer/child-build", "echo-msg", "--json")

	// Wire parent's echo-msg.args.msg to "built ${child-build.output.version}".
	// Uses the `set` pattern via file patching (same rationale as above).
	parentPath := filepath.Join(env.home, "drawers", "parent-flow", "drawer.json")
	pdata, _ := os.ReadFile(parentPath) // #nosec G304 — test scope
	var parent map[string]any
	_ = json.Unmarshal(pdata, &parent)
	steps := parent["steps"].([]any)
	for _, s := range steps {
		step := s.(map[string]any)
		if step["id"] == "echo-msg" {
			step["args"] = map[string]any{
				"msg": "built ${child-build.output.version}",
			}
		}
	}
	pout, _ := json.MarshalIndent(parent, "", "  ")
	if err := os.WriteFile(parentPath, pout, 0o600); err != nil {
		t.Fatalf("write parent: %v", err)
	}

	// Press the parent. Should:
	//  1. Run child-build, which runs emit-version
	//  2. Compute child's Return → {version: "1.2.3"}
	//  3. Pass that into parent's echo-msg.args.msg
	//  4. echo-msg prints {"got":"built 1.2.3"}
	r := env.run("drawer", "parent-flow", "press", "--json")
	if r.ExitCode != 0 {
		t.Fatalf("press: exit %d, stderr=%s, stdout=%s", r.ExitCode, r.Stderr, r.Stdout)
	}

	var resp struct {
		OK   bool `json:"ok"`
		Data struct {
			Status string `json:"status"`
			Steps  []struct {
				ID     string         `json:"id"`
				Status string         `json:"status"`
				Output map[string]any `json:"output"`
			} `json:"steps"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &resp); err != nil {
		t.Fatalf("parse: %v\nraw=%s", err, r.Stdout)
	}
	if !resp.OK || resp.Data.Status != "ok" {
		t.Fatalf("parent not ok: %s", r.Stdout)
	}
	if len(resp.Data.Steps) != 2 {
		t.Fatalf("expected 2 steps (child-build, echo-msg), got %d", len(resp.Data.Steps))
	}

	// Step 0 is the sub-drawer call; its output should be the child's
	// Return block, resolved.
	sub := resp.Data.Steps[0]
	if sub.ID != "child-build" {
		t.Errorf("first step id = %q, want child-build", sub.ID)
	}
	if sub.Output["version"] != "1.2.3" {
		t.Errorf("sub-drawer output.version = %v, want 1.2.3", sub.Output["version"])
	}

	// Step 1 is echo-msg; its output should contain the value that
	// flowed through the sub-drawer.
	echo := resp.Data.Steps[1]
	if echo.ID != "echo-msg" {
		t.Errorf("second step id = %q, want echo-msg", echo.ID)
	}
	if echo.Output["got"] != "built 1.2.3" {
		t.Errorf("final output.got = %v, want 'built 1.2.3'", echo.Output["got"])
	}

	// Child's own run history should be populated too — its trace
	// persisted separately from the parent's.
	childPressed := filepath.Join(env.home, "drawers", "child-build", "pressed")
	entries, _ := os.ReadDir(childPressed)
	if len(entries) == 0 {
		t.Errorf("child-build should have its own pressed/ history")
	}
}

// TestSubDrawer_SelfReferenceRejected verifies that
// `buttons drawer X add drawer/X` is rejected up-front.
func TestSubDrawer_SelfReferenceRejected(t *testing.T) {
	env := newTestEnv(t)

	env.run("drawer", "create", "loopy", "--json")
	r := env.run("drawer", "loopy", "add", "drawer/loopy", "--json")
	if r.ExitCode == 0 {
		t.Fatalf("expected failure on self-reference, got success: %s", r.Stdout)
	}
	if !strings.Contains(r.Stdout, "cannot include itself") {
		t.Errorf("expected self-reference error, got: %s", r.Stdout)
	}
}

// TestSubDrawer_MissingChildRejected verifies adding a drawer step
// for a non-existent drawer fails clearly.
func TestSubDrawer_MissingChildRejected(t *testing.T) {
	env := newTestEnv(t)
	env.run("drawer", "create", "parent", "--json")
	r := env.run("drawer", "parent", "add", "drawer/does-not-exist", "--json")
	if r.ExitCode == 0 {
		t.Fatal("expected failure on missing child drawer")
	}
	if !strings.Contains(r.Stdout, "DRAWER_NOT_FOUND") {
		t.Errorf("expected DRAWER_NOT_FOUND, got: %s", r.Stdout)
	}
}

// TestDrawerSet_WritesArgs verifies `buttons drawer X set
// STEP.args.FIELD=value` persists literal and ${ref} values so
// agents can wire step args without editing drawer.json by hand.
func TestDrawerSet_WritesArgs(t *testing.T) {
	env := newTestEnv(t)

	env.run("create", "echo-msg",
		"--runtime", "shell",
		"--code", `echo "{\"got\":\"$BUTTONS_ARG_MSG\"}"`,
		"--arg", "msg:string:required",
		"--json",
	)
	env.run("drawer", "create", "flow", "--json")
	env.run("drawer", "flow", "add", "echo-msg", "--json")

	// Literal value.
	r := env.run("drawer", "flow", "set", "echo-msg.args.msg=hello world", "--json")
	if r.ExitCode != 0 {
		t.Fatalf("set literal: exit %d, stderr=%s", r.ExitCode, r.Stderr)
	}

	// Press — should pick up the set value. Stdout is JSON-escaped
	// in the outer result envelope, so match the escaped form.
	r = env.run("drawer", "flow", "press", "--json")
	if r.ExitCode != 0 {
		t.Fatalf("press: exit %d, stderr=%s", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stdout, `hello world`) {
		t.Errorf("expected press output to contain 'hello world', got: %s", r.Stdout)
	}

	// ${ref} expression — references a drawer input.
	env.run("drawer", "flow", "set", `echo-msg.args.msg=built ${inputs.ver}`, "--json")
	r = env.run("drawer", "flow", "press", "ver=7.7.7", "--json")
	if r.ExitCode != 0 {
		t.Fatalf("press w/ ref: exit %d, stderr=%s, stdout=%s", r.ExitCode, r.Stderr, r.Stdout)
	}
	if !strings.Contains(r.Stdout, `built 7.7.7`) {
		t.Errorf("expected output to contain 'built 7.7.7', got: %s", r.Stdout)
	}
}

// TestForEach_IteratesNestedSteps verifies kind=for_each runs its
// nested Steps once per item in the Over array, with ${<as>.field}
// refs resolving against the current item. No dedicated CLI
// authoring surface yet — the test patches the drawer.json directly,
// which also exercises the schema round-trip.
func TestForEach_IteratesNestedSteps(t *testing.T) {
	env := newTestEnv(t)

	// List buttons: emitter produces a JSON array of strings.
	env.run("create", "list-names",
		"--runtime", "shell",
		"--code", `echo '{"names":["a","b","c"]}'`,
		"--json",
	)
	// Greeter: takes a name arg, echoes back.
	env.run("create", "greeter",
		"--runtime", "shell",
		"--code", `echo "{\"hi\":\"$BUTTONS_ARG_NAME\"}"`,
		"--arg", "name:string:required",
		"--json",
	)

	// Create drawer with list-names step + for_each wrapper around
	// greeter. We patch drawer.json to plant the for_each step
	// since the CLI doesn't yet author nested steps directly.
	env.run("drawer", "create", "loop-demo", "--json")
	env.run("drawer", "loop-demo", "add", "list-names", "--json")

	drawerPath := filepath.Join(env.home, "drawers", "loop-demo", "drawer.json")
	data, _ := os.ReadFile(drawerPath) // #nosec G304 — test scope
	var d map[string]any
	_ = json.Unmarshal(data, &d)
	steps := d["steps"].([]any)
	// Append a for_each step that iterates list-names.output.names
	// and runs greeter per item.
	forEach := map[string]any{
		"id":   "each-name",
		"kind": "for_each",
		"over": "${list-names.output.names}",
		"as":   "n",
		"steps": []any{
			map[string]any{
				"id":     "greet",
				"kind":   "button",
				"button": "greeter",
				"args": map[string]any{
					"name": "${n}",
				},
			},
		},
	}
	steps = append(steps, forEach)
	d["steps"] = steps
	out, _ := json.MarshalIndent(d, "", "  ")
	if err := os.WriteFile(drawerPath, out, 0o600); err != nil {
		t.Fatalf("write drawer: %v", err)
	}

	r := env.run("drawer", "loop-demo", "press", "--json")
	if r.ExitCode != 0 {
		t.Fatalf("press: exit %d, stderr=%s, stdout=%s", r.ExitCode, r.Stderr, r.Stdout)
	}

	// Expect the for_each step's output to report 3 iterations,
	// each successful, each producing {"hi": "<name>"}.
	var resp struct {
		OK   bool `json:"ok"`
		Data struct {
			Status string `json:"status"`
			Steps  []struct {
				ID     string         `json:"id"`
				Status string         `json:"status"`
				Output map[string]any `json:"output"`
			} `json:"steps"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &resp); err != nil {
		t.Fatalf("parse: %v\nraw=%s", err, r.Stdout)
	}
	if !resp.OK || resp.Data.Status != "ok" {
		t.Fatalf("drawer not ok: %s", r.Stdout)
	}

	// Find the for_each step in the output.
	var each *struct {
		ID     string         `json:"id"`
		Status string         `json:"status"`
		Output map[string]any `json:"output"`
	}
	for i := range resp.Data.Steps {
		if resp.Data.Steps[i].ID == "each-name" {
			each = &resp.Data.Steps[i]
			break
		}
	}
	if each == nil {
		t.Fatalf("each-name step not found in output: %s", r.Stdout)
	}
	if each.Status != "ok" {
		t.Errorf("each-name status = %q, want ok", each.Status)
	}
	totalAny, _ := each.Output["total"]
	if fmt.Sprintf("%v", totalAny) != "3" {
		t.Errorf("expected total=3, got %v", totalAny)
	}
	results, _ := each.Output["results"].([]any)
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
}

// TestForEach_MissingOverFails verifies the validator / executor
// catch a missing 'over' expression.
func TestForEach_MissingOverFails(t *testing.T) {
	env := newTestEnv(t)
	env.run("drawer", "create", "loopy", "--json")

	drawerPath := filepath.Join(env.home, "drawers", "loopy", "drawer.json")
	data, _ := os.ReadFile(drawerPath) // #nosec G304 — test scope
	var d map[string]any
	_ = json.Unmarshal(data, &d)
	d["steps"] = []any{
		map[string]any{
			"id":   "broken-loop",
			"kind": "for_each",
			"as":   "x",
			// over intentionally omitted
			"steps": []any{},
		},
	}
	out, _ := json.MarshalIndent(d, "", "  ")
	if err := os.WriteFile(drawerPath, out, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	r := env.run("drawer", "loopy", "press", "--json")
	if r.ExitCode == 0 {
		t.Fatal("expected failure on missing over")
	}
	if !strings.Contains(r.Stdout, "VALIDATION_ERROR") && !strings.Contains(r.Stdout, "no 'over'") {
		t.Errorf("expected missing-over error, got: %s", r.Stdout)
	}
}

// TestDrawerSet_RejectsBadTarget verifies typos in the path
// fail with a clear message instead of silently writing nowhere.
func TestDrawerSet_RejectsBadTarget(t *testing.T) {
	env := newTestEnv(t)
	env.run("drawer", "create", "flow", "--json")

	r := env.run("drawer", "flow", "set", "nope=1", "--json")
	if r.ExitCode == 0 {
		t.Fatal("expected failure on missing .args. in target")
	}
	if !strings.Contains(r.Stderr, "STEP.args.FIELD") && !strings.Contains(r.Stdout, "STEP.args.FIELD") {
		t.Errorf("expected usage hint, got stderr=%s stdout=%s", r.Stderr, r.Stdout)
	}
}
