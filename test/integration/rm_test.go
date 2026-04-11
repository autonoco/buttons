package integration

import (
	"encoding/json"
	"testing"
)

func TestDelete_Force(t *testing.T) {
	env := newTestEnv(t)
	script := env.createScript("test.sh")

	env.run("create", "test", "--file", script, "--json")

	res := env.run("delete", "test", "--force", "--json")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", res.ExitCode, res.Stderr)
	}

	resp := parseJSON(t, res.Stdout)
	if !resp.OK {
		t.Fatal("expected ok: true")
	}

	var data map[string]any
	json.Unmarshal(resp.Data, &data)
	if data["name"] != "test" {
		t.Errorf("name = %v, want test", data["name"])
	}
	if data["deleted"] != true {
		t.Errorf("deleted = %v, want true", data["deleted"])
	}

	// Verify it's gone
	listRes := env.run("list", "--json")
	buttons := parseButtonList(t, parseJSON(t, listRes.Stdout).Data)
	if len(buttons) != 0 {
		t.Errorf("expected empty list after delete, got %d", len(buttons))
	}
}

func TestDelete_NotFound(t *testing.T) {
	env := newTestEnv(t)

	res := env.run("delete", "ghost", "--force", "--json")
	if res.ExitCode == 0 {
		t.Fatal("expected non-zero exit")
	}

	resp := parseJSON(t, res.Stdout)
	if resp.Error.Code != "NOT_FOUND" {
		t.Errorf("code = %q, want NOT_FOUND", resp.Error.Code)
	}
}

func TestDelete_RmAlias(t *testing.T) {
	env := newTestEnv(t)
	script := env.createScript("test.sh")

	env.run("create", "test", "--file", script, "--json")

	// rm should work as an alias for delete
	res := env.run("rm", "test", "--force", "--json")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", res.ExitCode, res.Stderr)
	}

	resp := parseJSON(t, res.Stdout)
	if !resp.OK {
		t.Fatal("expected ok: true for rm alias")
	}
}

func TestDelete_JSONImpliesForce(t *testing.T) {
	env := newTestEnv(t)
	script := env.createScript("test.sh")

	env.run("create", "test", "--file", script, "--json")

	// In JSON mode, --force is implied (agents are non-interactive)
	res := env.run("delete", "test", "--json")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0 (JSON implies force), got %d: %s", res.ExitCode, res.Stderr)
	}

	resp := parseJSON(t, res.Stdout)
	if !resp.OK {
		t.Fatal("expected ok: true")
	}
}
