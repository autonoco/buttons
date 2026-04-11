package integration

import (
	"testing"
)

func TestStub_BoardReturnsJSON(t *testing.T) {
	env := newTestEnv(t)

	res := env.run("board", "--json")
	if res.ExitCode == 0 {
		t.Fatal("expected non-zero exit for stub command")
	}

	resp := parseJSON(t, res.Stdout)
	if resp.OK {
		t.Fatal("expected ok: false")
	}
	if resp.Error.Code != "NOT_IMPLEMENTED" {
		t.Errorf("code = %q, want NOT_IMPLEMENTED", resp.Error.Code)
	}
}

func TestStub_DrawerCreateReturnsJSON(t *testing.T) {
	env := newTestEnv(t)

	res := env.run("drawer", "create", "test", "--json")
	if res.ExitCode == 0 {
		t.Fatal("expected non-zero exit for stub command")
	}

	resp := parseJSON(t, res.Stdout)
	if resp.OK {
		t.Fatal("expected ok: false")
	}
	if resp.Error.Code != "NOT_IMPLEMENTED" {
		t.Errorf("code = %q, want NOT_IMPLEMENTED", resp.Error.Code)
	}
}

func TestStub_SmashReturnsJSON(t *testing.T) {
	env := newTestEnv(t)

	res := env.run("smash", "a", "b", "--json")
	if res.ExitCode == 0 {
		t.Fatal("expected non-zero exit for stub command")
	}

	resp := parseJSON(t, res.Stdout)
	if resp.OK {
		t.Fatal("expected ok: false")
	}
	if resp.Error.Code != "NOT_IMPLEMENTED" {
		t.Errorf("code = %q, want NOT_IMPLEMENTED", resp.Error.Code)
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
