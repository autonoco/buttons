package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOnError_HandlerDrawerRuns verifies that when a drawer fails,
// the on_error handler drawer is invoked automatically with a
// standard error payload. Uses a sentinel file as the side effect
// so we can detect the handler ran without mocking the runtime.
func TestOnError_HandlerDrawerRuns(t *testing.T) {
	env := newTestEnv(t)

	// Flaky button — always fails.
	env.run("create", "always-fail",
		"--runtime", "shell",
		"--code", "echo 'kaboom' >&2; exit 7",
		"--json",
	)
	// Recorder button — appends "handler-ran" + the error code
	// it received to a sentinel file. Uses drawer inputs (which
	// map to button args by name-match) so the error's code field
	// flows through.
	sentinel := filepath.Join(env.home, "handler.log")
	env.run("create", "record-error",
		"--runtime", "shell",
		"--code", `echo "handler-ran $BUTTONS_ARG_CODE" >> "`+sentinel+`"; echo '{"ok":true}'`,
		"--arg", "code:string:required",
		"--json",
	)
	env.run("drawer", "create", "notify-failures", "--json")
	env.run("drawer", "notify-failures", "add", "record-error", "--json")

	// Primary drawer: calls always-fail. Patch drawer.json to add
	// on_error pointing at notify-failures (CLI doesn't have a
	// dedicated verb for this yet).
	env.run("drawer", "create", "main-flow", "--json")
	env.run("drawer", "main-flow", "add", "always-fail", "--json")
	drawerPath := filepath.Join(env.home, "drawers", "main-flow", "drawer.json")
	data, _ := os.ReadFile(drawerPath) // #nosec G304 — test scope
	var d map[string]any
	_ = json.Unmarshal(data, &d)
	d["on_error"] = map[string]any{
		"drawer": "notify-failures",
	}
	// notify-failures's record-error button takes a `code` arg,
	// filled from the handler's `error.code` field via the
	// standard payload passed by the on_error invocation. The
	// drawer's own inputs route through to the button by
	// name-match, so we need the notify-failures drawer to accept
	// a `code` input mapped into record-error.args.code.
	out, _ := json.MarshalIndent(d, "", "  ")
	_ = os.WriteFile(drawerPath, out, 0o600)

	// Patch notify-failures so its record-error step pulls `code`
	// out of the standard error payload (inputs.error.code). No
	// top-level inputs declaration — the handler receives the
	// whole error envelope and references into it directly.
	handlerPath := filepath.Join(env.home, "drawers", "notify-failures", "drawer.json")
	data, _ = os.ReadFile(handlerPath) // #nosec G304 — test scope
	var handler map[string]any
	_ = json.Unmarshal(data, &handler)
	steps := handler["steps"].([]any)
	for _, s := range steps {
		step := s.(map[string]any)
		if step["id"] == "record-error" {
			step["args"] = map[string]any{
				"code": "${inputs.error.code}",
			}
		}
	}
	handler["steps"] = steps
	hout, _ := json.MarshalIndent(handler, "", "  ")
	_ = os.WriteFile(handlerPath, hout, 0o600)

	// Press the main drawer. Expect failure, and expect the
	// handler to have written to the sentinel.
	r := env.run("drawer", "main-flow", "press", "--json")
	if r.ExitCode == 0 {
		t.Fatalf("expected main-flow failure, got success: %s", r.Stdout)
	}

	// Give the handler a moment in case of timing — should be
	// synchronous so this is belt-and-suspenders.
	raw, err := os.ReadFile(sentinel) // #nosec G304 — test scope
	if err != nil {
		t.Fatalf("sentinel not written: %v", err)
	}
	if !strings.Contains(string(raw), "handler-ran") {
		t.Errorf("handler didn't run: sentinel = %q", string(raw))
	}
	// error.code from a shell-button failure is SCRIPT_ERROR.
	if !strings.Contains(string(raw), "SCRIPT_ERROR") {
		t.Errorf("handler didn't receive error.code: sentinel = %q", string(raw))
	}
}

// TestWait_Duration verifies kind=wait pauses for the requested
// duration and reports the actual wait in output.waited_ms.
func TestWait_Duration(t *testing.T) {
	env := newTestEnv(t)
	env.run("drawer", "create", "pause-demo", "--json")

	// Use the shorthand to add a wait:200ms step.
	r := env.run("drawer", "pause-demo", "add", "wait:200ms", "--json")
	if r.ExitCode != 0 {
		t.Fatalf("add wait: exit %d, stderr=%s", r.ExitCode, r.Stderr)
	}

	r = env.run("drawer", "pause-demo", "press", "--json")
	if r.ExitCode != 0 {
		t.Fatalf("press: exit %d, stderr=%s, stdout=%s", r.ExitCode, r.Stderr, r.Stdout)
	}

	var resp struct {
		OK   bool `json:"ok"`
		Data struct {
			Status string `json:"status"`
			Steps  []struct {
				ID         string         `json:"id"`
				Status     string         `json:"status"`
				DurationMs int64          `json:"duration_ms"`
				Output     map[string]any `json:"output"`
			} `json:"steps"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &resp); err != nil {
		t.Fatalf("parse: %v\nraw=%s", err, r.Stdout)
	}
	if !resp.OK || resp.Data.Status != "ok" {
		t.Fatalf("wait drawer not ok: %s", r.Stdout)
	}
	if len(resp.Data.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(resp.Data.Steps))
	}
	step := resp.Data.Steps[0]
	if step.Status != "ok" {
		t.Errorf("wait status = %q, want ok", step.Status)
	}
	if step.DurationMs < 150 {
		// Allow some slack below 200ms in case the system clock is
		// slightly off. If it's < 150ms the wait didn't actually fire.
		t.Errorf("wait duration_ms = %d, expected ≥150", step.DurationMs)
	}
}

// TestWait_MissingDurationAndUntilFails verifies the validator /
// executor catches a wait step that has neither duration nor until.
func TestWait_MissingDurationAndUntilFails(t *testing.T) {
	env := newTestEnv(t)
	env.run("drawer", "create", "bad-wait", "--json")

	drawerPath := filepath.Join(env.home, "drawers", "bad-wait", "drawer.json")
	data, _ := os.ReadFile(drawerPath) // #nosec G304 — test scope
	var d map[string]any
	_ = json.Unmarshal(data, &d)
	d["steps"] = []any{
		map[string]any{
			"id":   "broken",
			"kind": "wait",
			// neither duration nor until set
		},
	}
	out, _ := json.MarshalIndent(d, "", "  ")
	_ = os.WriteFile(drawerPath, out, 0o600)

	r := env.run("drawer", "bad-wait", "press", "--json")
	if r.ExitCode == 0 {
		t.Fatal("expected failure on wait without duration/until")
	}
	if !strings.Contains(r.Stdout, "duration") && !strings.Contains(r.Stdout, "until") {
		t.Errorf("expected missing-duration error, got: %s", r.Stdout)
	}
}
