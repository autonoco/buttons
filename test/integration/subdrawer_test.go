package integration

import (
	"encoding/json"
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
