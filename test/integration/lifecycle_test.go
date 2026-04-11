package integration

import (
	"testing"
)

func TestLifecycle_CreateListRemove(t *testing.T) {
	env := newTestEnv(t)
	script := env.createScript("health.sh")

	// Create with args and description
	res := env.run("create", "health-check", "--file", script,
		"--description", "Check service health",
		"--timeout", "30",
		"--arg", "url:string:required",
		"--arg", "verbose:bool:optional",
		"--json")
	if res.ExitCode != 0 {
		t.Fatalf("create failed: %d: %s", res.ExitCode, res.Stderr)
	}

	btn := parseButton(t, parseJSON(t, res.Stdout).Data)
	if btn.Name != "health-check" {
		t.Errorf("name = %q, want health-check", btn.Name)
	}
	if btn.Description != "Check service health" {
		t.Errorf("description = %q", btn.Description)
	}
	if btn.TimeoutSeconds != 30 {
		t.Errorf("timeout = %d, want 30", btn.TimeoutSeconds)
	}
	if len(btn.Args) != 2 {
		t.Errorf("args count = %d, want 2", len(btn.Args))
	}

	// List should show 1 button
	listRes := env.run("list", "--json")
	if listRes.ExitCode != 0 {
		t.Fatalf("list failed: %d", listRes.ExitCode)
	}
	buttons := parseButtonList(t, parseJSON(t, listRes.Stdout).Data)
	if len(buttons) != 1 {
		t.Fatalf("expected 1 button, got %d", len(buttons))
	}
	if buttons[0].SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", buttons[0].SchemaVersion)
	}

	// Remove
	rmRes := env.run("rm", "health-check", "--force", "--json")
	if rmRes.ExitCode != 0 {
		t.Fatalf("rm failed: %d", rmRes.ExitCode)
	}

	// List should be empty
	listRes2 := env.run("list", "--json")
	buttons2 := parseButtonList(t, parseJSON(t, listRes2.Stdout).Data)
	if len(buttons2) != 0 {
		t.Errorf("expected empty list after rm, got %d", len(buttons2))
	}
}
