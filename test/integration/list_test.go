package integration

import (
	"testing"
)

func TestList_Empty(t *testing.T) {
	env := newTestEnv(t)

	res := env.run("list", "--json")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", res.ExitCode, res.Stderr)
	}

	resp := parseJSON(t, res.Stdout)
	if !resp.OK {
		t.Fatal("expected ok: true")
	}

	buttons := parseButtonList(t, resp.Data)
	if len(buttons) != 0 {
		t.Errorf("expected empty list, got %d", len(buttons))
	}
}

func TestList_AfterCreate(t *testing.T) {
	env := newTestEnv(t)
	script := env.createScript("test.sh")

	env.run("create", "zebra", "--file", script, "--json")
	env.run("create", "alpha", "--file", script, "--json")

	res := env.run("list", "--json")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d", res.ExitCode)
	}

	buttons := parseButtonList(t, parseJSON(t, res.Stdout).Data)
	if len(buttons) != 2 {
		t.Fatalf("expected 2 buttons, got %d", len(buttons))
	}
	if buttons[0].Name != "alpha" {
		t.Errorf("first button = %q, want %q", buttons[0].Name, "alpha")
	}
	if buttons[1].Name != "zebra" {
		t.Errorf("second button = %q, want %q", buttons[1].Name, "zebra")
	}
}

func TestList_HumanReadable(t *testing.T) {
	env := newTestEnv(t)
	script := env.createScript("test.sh")

	env.run("create", "mybutton", "--file", script, "--json")

	// Run without --json. Since we're piped, it will auto-detect non-TTY
	// and output JSON anyway. But we can verify the command succeeds.
	res := env.run("list")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", res.ExitCode, res.Stderr)
	}
}
