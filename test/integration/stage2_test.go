package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestIdempotency_CachedResult verifies that a second press with
// the same --idempotency-key returns the cached result without
// re-executing the button.
func TestIdempotency_CachedResult(t *testing.T) {
	env := newTestEnv(t)
	env.run("create", "counter",
		"--runtime", "shell",
		// Increment a sentinel file each press so we can detect
		// re-execution. If cached, the file size stays constant.
		"--code", `echo "tick" >> $BUTTONS_HOME/counter.log; echo '{"ok":true}'`,
		"--json",
	)

	r := env.run("press", "counter", "--idempotency-key=abc", "--json")
	if r.ExitCode != 0 {
		t.Fatalf("first press: exit %d, stderr=%s", r.ExitCode, r.Stderr)
	}

	// Second press with same key — should NOT re-execute the script.
	r = env.run("press", "counter", "--idempotency-key=abc", "--json")
	if r.ExitCode != 0 {
		t.Fatalf("second press: exit %d", r.ExitCode)
	}

	// The counter file should have exactly 1 line.
	logPath := filepath.Join(env.home, "counter.log")
	data, err := os.ReadFile(logPath) // #nosec G304 — test scope
	if err != nil {
		t.Fatalf("read counter log: %v", err)
	}
	lines := strings.Count(strings.TrimRight(string(data), "\n"), "\n") + 1
	if lines != 1 {
		t.Errorf("expected 1 execution, got %d", lines)
	}

	// Third press WITHOUT the key — should execute normally.
	r = env.run("press", "counter", "--json")
	if r.ExitCode != 0 {
		t.Fatalf("third press: exit %d", r.ExitCode)
	}
	data, _ = os.ReadFile(logPath) // #nosec G304 — test scope
	lines = strings.Count(strings.TrimRight(string(data), "\n"), "\n") + 1
	if lines != 2 {
		t.Errorf("expected 2 executions after bypassing cache, got %d", lines)
	}
}

// TestProgressPath_ExportsEnvVar verifies the engine exports
// BUTTONS_PROGRESS_PATH and the file is created and reachable.
func TestProgressPath_ExportsEnvVar(t *testing.T) {
	env := newTestEnv(t)
	env.run("create", "reporter",
		"--runtime", "shell",
		"--code", `echo "{\"path\":\"$BUTTONS_PROGRESS_PATH\"}"`,
		"--json",
	)

	r := env.run("press", "reporter", "--json")
	if r.ExitCode != 0 {
		t.Fatalf("press: exit %d, stderr=%s", r.ExitCode, r.Stderr)
	}
	var resp struct {
		OK   bool `json:"ok"`
		Data struct {
			Stdout       string `json:"stdout"`
			ProgressPath string `json:"progress_path"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if resp.Data.ProgressPath == "" {
		t.Fatal("result.progress_path is empty")
	}
	if !strings.Contains(resp.Data.Stdout, resp.Data.ProgressPath) {
		t.Errorf("script didn't see BUTTONS_PROGRESS_PATH: stdout=%s", resp.Data.Stdout)
	}
	if _, err := os.Stat(resp.Data.ProgressPath); err != nil {
		t.Errorf("progress file not created: %v", err)
	}
}

// TestFailure_ShowsInSummary verifies a failed press lands in the
// button's history and is surfaced by `buttons summary --json`
// under recent_failures. No separate DLQ — failures live where
// the rest of history lives.
func TestFailure_ShowsInSummary(t *testing.T) {
	env := newTestEnv(t)
	env.run("create", "always-fail",
		"--runtime", "shell",
		"--code", "echo 'kaboom' >&2; exit 7",
		"--json",
	)

	r := env.run("press", "always-fail", "--json")
	if r.ExitCode == 0 {
		t.Fatalf("expected failure, got success")
	}

	r = env.run("summary", "--json")
	if r.ExitCode != 0 {
		t.Fatalf("summary: exit %d", r.ExitCode)
	}
	if !strings.Contains(r.Stdout, "always-fail") {
		t.Errorf("summary should mention the failed button: %s", r.Stdout)
	}
	if !strings.Contains(r.Stdout, "recent_failures") {
		t.Errorf("summary should have a recent_failures bucket: %s", r.Stdout)
	}
}

// TestCELExpression_InDrawerStepArg verifies that a CEL expression
// stored in a drawer step arg (via manual drawer.json edit) is
// evaluated at press time. This exercises the google/cel-go swap.
func TestCELExpression_InDrawerStepArg(t *testing.T) {
	env := newTestEnv(t)

	env.run("create", "emitter",
		"--runtime", "shell",
		"--code", `echo '{"count": 5}'`,
		"--json",
	)
	env.run("create", "receiver",
		"--runtime", "shell",
		"--code", `echo "{\"got\":\"$BUTTONS_ARG_DOUBLED\"}"`,
		"--arg", "doubled:string:required",
		"--json",
	)
	env.run("drawer", "create", "math", "--json")
	env.run("drawer", "math", "add", "emitter", "receiver", "--json")

	// Manually edit drawer.json to plant a CEL expression that uses
	// string interpolation: exercises the google/cel-go evaluation
	// path in mixed-literal mode (template string + expression).
	drawerPath := filepath.Join(env.home, "drawers", "math", "drawer.json")
	data, err := os.ReadFile(drawerPath) // #nosec G304 — test scope
	if err != nil {
		t.Fatalf("read drawer: %v", err)
	}
	var d map[string]any
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatalf("parse drawer: %v", err)
	}
	steps := d["steps"].([]any)
	for _, s := range steps {
		step := s.(map[string]any)
		if step["id"] == "receiver" {
			step["args"] = map[string]any{
				"doubled": `count-is-${emitter.output.count}`,
			}
		}
	}
	out, _ := json.MarshalIndent(d, "", "  ")
	if err := os.WriteFile(drawerPath, out, 0o600); err != nil {
		t.Fatalf("write drawer: %v", err)
	}

	r := env.run("drawer", "math", "press", "--json")
	if r.ExitCode != 0 {
		t.Fatalf("press: exit %d, stderr=%s, stdout=%s", r.ExitCode, r.Stderr, r.Stdout)
	}
	if !strings.Contains(r.Stdout, `count-is-5`) {
		t.Errorf("CEL reference didn't resolve through the resolver: %s", r.Stdout)
	}
}
