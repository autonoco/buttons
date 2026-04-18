package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCreate_Basic(t *testing.T) {
	env := newTestEnv(t)
	env.createScript("test.sh")

	res := env.run("create", "test", "--file", filepath.Join(env.home, "test.sh"), "--json")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", res.ExitCode, res.Stderr)
	}

	resp := parseJSON(t, res.Stdout)
	if !resp.OK {
		t.Fatal("expected ok: true")
	}

	btn := parseButton(t, resp.Data)
	if btn.SchemaVersion != 2 {
		t.Errorf("schema_version = %d, want 2", btn.SchemaVersion)
	}
	if btn.Name != "test" {
		t.Errorf("name = %q, want %q", btn.Name, "test")
	}
	if btn.Runtime != "shell" {
		t.Errorf("runtime = %q, want %q", btn.Runtime, "shell")
	}
	if btn.TimeoutSeconds != 300 {
		t.Errorf("timeout = %d, want 300 (default)", btn.TimeoutSeconds)
	}
	if btn.MCPEnabled {
		t.Error("mcp_enabled should be false")
	}
	if btn.CreatedAt == "" || btn.UpdatedAt == "" {
		t.Error("timestamps should be set")
	}
	if btn.CreatedAt != btn.UpdatedAt {
		t.Error("created_at and updated_at should be equal on create")
	}
}

func TestCreate_WithArgs(t *testing.T) {
	env := newTestEnv(t)
	script := env.createScript("test.sh")

	res := env.run("create", "test", "--file", script, "--arg", "url:string:required", "--arg", "verbose:bool:optional", "--json")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", res.ExitCode, res.Stderr)
	}

	btn := parseButton(t, parseJSON(t, res.Stdout).Data)
	if len(btn.Args) != 2 {
		t.Fatalf("expected 2 args, got %d", len(btn.Args))
	}
	if btn.Args[0].Name != "url" || btn.Args[0].Type != "string" || !btn.Args[0].Required {
		t.Errorf("arg[0] = %+v, want url:string:required", btn.Args[0])
	}
	if btn.Args[1].Name != "verbose" || btn.Args[1].Type != "bool" || btn.Args[1].Required {
		t.Errorf("arg[1] = %+v, want verbose:bool:optional", btn.Args[1])
	}
}

func TestCreate_WithDescription(t *testing.T) {
	env := newTestEnv(t)
	script := env.createScript("test.sh")

	res := env.run("create", "test", "--file", script, "--description", "Run tests", "--json")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d", res.ExitCode)
	}

	btn := parseButton(t, parseJSON(t, res.Stdout).Data)
	if btn.Description != "Run tests" {
		t.Errorf("description = %q, want %q", btn.Description, "Run tests")
	}
}

func TestCreate_WithTimeout(t *testing.T) {
	env := newTestEnv(t)
	script := env.createScript("test.sh")

	res := env.run("create", "test", "--file", script, "--timeout", "30", "--json")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d", res.ExitCode)
	}

	btn := parseButton(t, parseJSON(t, res.Stdout).Data)
	if btn.TimeoutSeconds != 30 {
		t.Errorf("timeout = %d, want 30", btn.TimeoutSeconds)
	}
}

func TestCreate_FilePermissions(t *testing.T) {
	env := newTestEnv(t)
	script := env.createScript("test.sh")

	res := env.run("create", "test", "--file", script, "--json")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d", res.ExitCode)
	}

	btnPath := filepath.Join(env.home, "buttons", "test", "button.json")
	info, err := os.Stat(btnPath)
	if err != nil {
		t.Fatalf("button file not found: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("file permissions = %o, want 0600", info.Mode().Perm())
	}
}

func TestCreate_Defaults(t *testing.T) {
	env := newTestEnv(t)
	script := env.createScript("test.sh")

	res := env.run("create", "test", "--file", script, "--json")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d", res.ExitCode)
	}

	// Read raw JSON to check env field
	btnPath := filepath.Join(env.home, "buttons", "test", "button.json")
	data, _ := os.ReadFile(btnPath)
	var raw map[string]json.RawMessage
	json.Unmarshal(data, &raw)

	// env should be empty object
	if string(raw["env"]) != "{}" {
		t.Errorf("env = %s, want {}", raw["env"])
	}
	// mcp_enabled should be false
	if string(raw["mcp_enabled"]) != "false" {
		t.Errorf("mcp_enabled = %s, want false", raw["mcp_enabled"])
	}
}

func TestCreate_DuplicateName(t *testing.T) {
	env := newTestEnv(t)
	script := env.createScript("test.sh")

	env.run("create", "test", "--file", script, "--json")
	res := env.run("create", "test", "--file", script, "--json")

	if res.ExitCode == 0 {
		t.Fatal("expected non-zero exit for duplicate")
	}
	resp := parseJSON(t, res.Stdout)
	if resp.OK {
		t.Fatal("expected ok: false")
	}
	if resp.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", resp.Error.Code)
	}
}

func TestCreate_FileNotExist(t *testing.T) {
	env := newTestEnv(t)

	res := env.run("create", "test", "--file", "/nonexistent.sh", "--json")
	if res.ExitCode == 0 {
		t.Fatal("expected non-zero exit")
	}
	resp := parseJSON(t, res.Stdout)
	if resp.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", resp.Error.Code)
	}
}

func TestCreate_Slugify(t *testing.T) {
	env := newTestEnv(t)
	script := env.createScript("test.sh")

	res := env.run("create", "MyButton", "--file", script, "--json")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d", res.ExitCode)
	}

	btn := parseButton(t, parseJSON(t, res.Stdout).Data)
	if btn.Name != "mybutton" {
		t.Errorf("name = %q, want %q", btn.Name, "mybutton")
	}
}

func TestCreate_PathTraversal(t *testing.T) {
	env := newTestEnv(t)
	script := env.createScript("test.sh")

	// Names with path traversal characters should be rejected
	res := env.run("create", "../evil", "--file", script, "--json")
	if res.ExitCode == 0 {
		t.Fatal("expected non-zero exit for path traversal name")
	}
	resp := parseJSON(t, res.Stdout)
	if resp.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", resp.Error.Code)
	}

	// Names with slashes should also be rejected
	res2 := env.run("create", "foo/bar", "--file", script, "--json")
	if res2.ExitCode == 0 {
		t.Fatal("expected non-zero exit for name with slashes")
	}
}

func TestCreate_MalformedArg_MissingSegment(t *testing.T) {
	env := newTestEnv(t)
	script := env.createScript("test.sh")

	res := env.run("create", "test", "--file", script, "--arg", "url:string", "--json")
	if res.ExitCode == 0 {
		t.Fatal("expected non-zero exit")
	}
	resp := parseJSON(t, res.Stdout)
	if resp.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", resp.Error.Code)
	}
}

func TestCreate_MalformedArg_EmptyType(t *testing.T) {
	env := newTestEnv(t)
	script := env.createScript("test.sh")

	res := env.run("create", "test", "--file", script, "--arg", "url::required", "--json")
	if res.ExitCode == 0 {
		t.Fatal("expected non-zero exit")
	}
	resp := parseJSON(t, res.Stdout)
	if resp.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", resp.Error.Code)
	}
}

func TestCreate_MalformedArg_InvalidType(t *testing.T) {
	env := newTestEnv(t)
	script := env.createScript("test.sh")

	res := env.run("create", "test", "--file", script, "--arg", "url:float:required", "--json")
	if res.ExitCode == 0 {
		t.Fatal("expected non-zero exit")
	}
	resp := parseJSON(t, res.Stdout)
	if resp.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", resp.Error.Code)
	}
}

func TestCreate_MalformedArg_DuplicateNames(t *testing.T) {
	env := newTestEnv(t)
	script := env.createScript("test.sh")

	res := env.run("create", "test", "--file", script, "--arg", "url:string:required", "--arg", "url:int:optional", "--json")
	if res.ExitCode == 0 {
		t.Fatal("expected non-zero exit")
	}
	resp := parseJSON(t, res.Stdout)
	if resp.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", resp.Error.Code)
	}
}
