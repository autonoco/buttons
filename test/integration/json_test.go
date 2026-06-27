package integration

import (
	"encoding/json"
	"testing"
)

func TestBoard_JSONNotApplicable(t *testing.T) {
	env := newTestEnv(t)

	// `board` is an interactive TUI — invoking it with --json should refuse
	// cleanly with NOT_APPLICABLE rather than attempting to render.
	res := env.run("board", "--json")
	if res.ExitCode == 0 {
		t.Fatal("expected non-zero exit for board --json")
	}

	resp := parseJSON(t, res.Stdout)
	if resp.OK {
		t.Fatal("expected ok: false")
	}
	if resp.Error.Code != "NOT_APPLICABLE" {
		t.Errorf("code = %q, want NOT_APPLICABLE", resp.Error.Code)
	}
}

func TestDrawerCreateReturnsJSON(t *testing.T) {
	env := newTestEnv(t)

	res := env.run("drawer", "create", "test", "--json")
	if res.ExitCode != 0 {
		t.Fatalf("expected zero exit, got %d, stderr=%s", res.ExitCode, res.Stderr)
	}

	resp := parseJSON(t, res.Stdout)
	if !resp.OK {
		t.Fatalf("expected ok: true, got %+v", resp.Error)
	}
}

func TestSmashReturnsJSON(t *testing.T) {
	env := newTestEnv(t)

	// smash runs each named button in parallel and returns a per-button
	// report. With no such buttons, every press fails: the command exits
	// non-zero and data.failures equals the number of names. The envelope
	// ok stays true — smash is a batch reporter, so per-button outcomes live
	// in data.results[].error and the aggregate in data.failures.
	res := env.run("smash", "a", "b", "--json")
	if res.ExitCode == 0 {
		t.Fatal("expected non-zero exit when every smashed button fails")
	}

	var resp struct {
		OK   bool `json:"ok"`
		Data struct {
			Total    int `json:"total"`
			Failures int `json:"failures"`
			Results  []struct {
				Button string `json:"button"`
				Error  string `json:"error"`
			} `json:"results"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &resp); err != nil {
		t.Fatalf("smash --json did not emit valid JSON: %v\n%s", err, res.Stdout)
	}
	if resp.Data.Total != 2 || resp.Data.Failures != 2 {
		t.Fatalf("want total=2 failures=2, got total=%d failures=%d", resp.Data.Total, resp.Data.Failures)
	}
	if len(resp.Data.Results) != 2 {
		t.Fatalf("want 2 per-button results, got %d", len(resp.Data.Results))
	}
	for _, r := range resp.Data.Results {
		if r.Error == "" {
			t.Errorf("button %q: expected an error for a missing button", r.Button)
		}
	}
}

func TestRoot_NoArgsReturnsJSON(t *testing.T) {
	env := newTestEnv(t)

	// Running buttons with no args in non-TTY (piped) mode returns the board listing as JSON
	res := env.run("--json")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", res.ExitCode, res.Stderr)
	}

	resp := parseJSON(t, res.Stdout)
	if !resp.OK {
		t.Fatal("expected ok: true")
	}
}
