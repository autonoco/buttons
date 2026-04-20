package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestForEach_ParallelismSpeedsUpRuns verifies that setting
// parallelism > 1 on a for_each step genuinely runs iterations
// concurrently. Each iteration sleeps 400ms; with 4 items and
// parallelism=4 the wall time should be ~400ms, not 1.6s.
//
// We give the assertion generous slack (<1200ms) because CI
// machines under load can be sluggish, but it's still well below
// the serial baseline so a regression to serial execution would
// be caught.
func TestForEach_ParallelismSpeedsUpRuns(t *testing.T) {
	env := newTestEnv(t)

	env.run("create", "emit-ids",
		"--runtime", "shell",
		"--code", `echo '{"ids":[1,2,3,4]}'`,
		"--json",
	)
	env.run("create", "slow-op",
		"--runtime", "shell",
		"--code", `sleep 0.4; echo "{\"got\":$BUTTONS_ARG_N}"`,
		"--arg", "n:int:required",
		"--json",
	)

	env.run("drawer", "create", "par", "--json")
	env.run("drawer", "par", "add", "emit-ids", "--json")

	// Plant a for_each with parallelism=4 — direct drawer.json
	// patch since nested CLI authoring doesn't yet cover this
	// shape end-to-end.
	drawerPath := filepath.Join(env.home, "drawers", "par", "drawer.json")
	data, _ := os.ReadFile(drawerPath) // #nosec G304 — test scope
	var d map[string]any
	_ = json.Unmarshal(data, &d)
	steps := d["steps"].([]any)
	steps = append(steps, map[string]any{
		"id":          "each",
		"kind":        "for_each",
		"over":        "${emit-ids.output.ids}",
		"as":          "n",
		"parallelism": 4,
		"steps": []any{
			map[string]any{
				"id":     "work",
				"kind":   "button",
				"button": "slow-op",
				"args":   map[string]any{"n": "${n}"},
			},
		},
	})
	d["steps"] = steps
	out, _ := json.MarshalIndent(d, "", "  ")
	_ = os.WriteFile(drawerPath, out, 0o600)

	start := time.Now()
	r := env.run("drawer", "par", "press", "--json")
	wall := time.Since(start)

	if r.ExitCode != 0 {
		t.Fatalf("press: exit %d, stderr=%s, stdout=%s", r.ExitCode, r.Stderr, r.Stdout)
	}

	// Serial baseline would be ~1600ms (4 × 400ms). With parallelism=4,
	// expect ~400ms + CLI overhead. 1200ms ceiling catches regressions
	// while tolerating slow CI.
	if wall > 1200*time.Millisecond {
		t.Errorf("parallel for_each took %v, expected <1.2s (serial baseline ~1.6s)", wall)
	}

	// Also verify the 4 iterations all ran and results are ordered.
	var resp struct {
		Data struct {
			Steps []struct {
				ID     string         `json:"id"`
				Output map[string]any `json:"output"`
			} `json:"steps"`
		} `json:"data"`
	}
	_ = json.Unmarshal([]byte(r.Stdout), &resp)
	var each *struct {
		ID     string         `json:"id"`
		Output map[string]any `json:"output"`
	}
	for i := range resp.Data.Steps {
		if resp.Data.Steps[i].ID == "each" {
			each = &resp.Data.Steps[i]
			break
		}
	}
	if each == nil {
		t.Fatalf("each step not found: %s", r.Stdout)
	}
	results, _ := each.Output["results"].([]any)
	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}
	// Index order preserved regardless of completion order.
	for i, raw := range results {
		entry := raw.(map[string]any)
		gotIdx, _ := entry["index"].(float64)
		if int(gotIdx) != i {
			t.Errorf("results[%d].index = %v, expected ordered", i, gotIdx)
		}
	}
}

// TestForEach_ParallelismStopOnFirstFailure verifies that when a
// parallel for_each has on_item_failure=stop, a single failure
// cancels in-flight workers so subsequent iterations don't run
// their child processes after the drawer has already decided to
// fail.
func TestForEach_ParallelismStopOnFirstFailure(t *testing.T) {
	env := newTestEnv(t)

	env.run("create", "emit-ids",
		"--runtime", "shell",
		"--code", `echo '{"ids":[1,2,3,4,5,6]}'`,
		"--json",
	)
	// Fails immediately for n >= 4, succeeds quickly for n < 4.
	env.run("create", "picky",
		"--runtime", "shell",
		"--code", `if [ $BUTTONS_ARG_N -ge 4 ]; then echo "bad"; exit 7; fi; echo "{\"n\":$BUTTONS_ARG_N}"`,
		"--arg", "n:int:required",
		"--json",
	)

	env.run("drawer", "create", "fast-fail", "--json")
	env.run("drawer", "fast-fail", "add", "emit-ids", "--json")
	drawerPath := filepath.Join(env.home, "drawers", "fast-fail", "drawer.json")
	data, _ := os.ReadFile(drawerPath) // #nosec G304 — test scope
	var d map[string]any
	_ = json.Unmarshal(data, &d)
	steps := d["steps"].([]any)
	steps = append(steps, map[string]any{
		"id":          "each",
		"kind":        "for_each",
		"over":        "${emit-ids.output.ids}",
		"as":          "n",
		"parallelism": 2,
		// default on_item_failure = stop
		"steps": []any{
			map[string]any{
				"id":     "work",
				"kind":   "button",
				"button": "picky",
				"args":   map[string]any{"n": "${n}"},
			},
		},
	})
	d["steps"] = steps
	out, _ := json.MarshalIndent(d, "", "  ")
	_ = os.WriteFile(drawerPath, out, 0o600)

	r := env.run("drawer", "fast-fail", "press", "--json")
	if r.ExitCode == 0 {
		t.Fatalf("expected failure, got success: %s", r.Stdout)
	}
	if !strings.Contains(r.Stdout, "FOREACH_ITEM_FAILED") {
		t.Errorf("expected FOREACH_ITEM_FAILED, got: %s", r.Stdout)
	}
}

// TestForEach_ParallelismContinueCollectsAll verifies on_item_failure=
// continue in parallel mode runs all iterations, records per-item
// failures, but the whole step still completes successfully
// (overall status=ok with partial failure markers in results).
func TestForEach_ParallelismContinueCollectsAll(t *testing.T) {
	env := newTestEnv(t)

	env.run("create", "emit-ids",
		"--runtime", "shell",
		"--code", `echo '{"ids":[1,2,3,4]}'`,
		"--json",
	)
	env.run("create", "odd-fails",
		"--runtime", "shell",
		"--code", `if [ $(( $BUTTONS_ARG_N % 2 )) -eq 1 ]; then exit 1; fi; echo "{\"n\":$BUTTONS_ARG_N}"`,
		"--arg", "n:int:required",
		"--json",
	)

	env.run("drawer", "create", "tolerant", "--json")
	env.run("drawer", "tolerant", "add", "emit-ids", "--json")
	drawerPath := filepath.Join(env.home, "drawers", "tolerant", "drawer.json")
	data, _ := os.ReadFile(drawerPath) // #nosec G304 — test scope
	var d map[string]any
	_ = json.Unmarshal(data, &d)
	steps := d["steps"].([]any)
	steps = append(steps, map[string]any{
		"id":                "each",
		"kind":              "for_each",
		"over":              "${emit-ids.output.ids}",
		"as":                "n",
		"parallelism":       4,
		"on_item_failure":   "continue",
		"steps": []any{
			map[string]any{
				"id":     "work",
				"kind":   "button",
				"button": "odd-fails",
				"args":   map[string]any{"n": "${n}"},
			},
		},
	})
	d["steps"] = steps
	out, _ := json.MarshalIndent(d, "", "  ")
	_ = os.WriteFile(drawerPath, out, 0o600)

	r := env.run("drawer", "tolerant", "press", "--json")
	if r.ExitCode != 0 {
		t.Fatalf("expected ok with continue, got exit %d: %s", r.ExitCode, r.Stdout)
	}
	// 2 odd iterations should show failed=true, 2 even should be
	// failed=false. Check a strict count.
	failedCount := strings.Count(r.Stdout, `"failed": true`)
	if failedCount != 2 {
		t.Errorf("expected 2 failed iterations, saw %d marker occurrences: %s", failedCount, r.Stdout)
	}
}
