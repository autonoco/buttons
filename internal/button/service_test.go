package button

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func setupTestEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	buttonsDir := filepath.Join(home, "buttons")
	if err := os.MkdirAll(buttonsDir, 0700); err != nil {
		t.Fatalf("failed to create buttons dir: %v", err)
	}
	return home
}

func createTestScript(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "test.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho hello"), 0755); err != nil {
		t.Fatalf("failed to create test script: %v", err)
	}
	return path
}

func TestService_Create(t *testing.T) {
	home := setupTestEnv(t)
	script := createTestScript(t, home)
	svc := NewService()

	btn, err := svc.Create(CreateOpts{
		Name:           "test",
		FilePath:       script,
		TimeoutSeconds: 60,
		Args:           []ArgDef{{Name: "url", Type: "string", Required: true}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if btn.SchemaVersion != 2 {
		t.Errorf("schema_version = %d, want 2", btn.SchemaVersion)
	}
	if btn.Runtime != "shell" {
		t.Errorf("runtime = %q, want %q", btn.Runtime, "shell")
	}
	if btn.TimeoutSeconds != 60 {
		t.Errorf("timeout = %d, want 60", btn.TimeoutSeconds)
	}
	if btn.MCPEnabled {
		t.Error("mcp_enabled should be false")
	}
	if btn.CreatedAt.IsZero() || btn.UpdatedAt.IsZero() {
		t.Error("timestamps should be set")
	}
	if !btn.CreatedAt.Equal(btn.UpdatedAt) {
		t.Error("created_at and updated_at should be equal on create")
	}
	if btn.CreatedAt.Location().String() != "UTC" {
		t.Error("timestamps should be UTC")
	}

	// Verify file permissions
	specPath := filepath.Join(home, "buttons", "test", "button.json")
	info, err := os.Stat(specPath)
	if err != nil {
		t.Fatalf("button spec not found: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("file permissions = %o, want 0600", info.Mode().Perm())
	}

	// Verify code file was copied
	codePath := filepath.Join(home, "buttons", "test", "main.sh")
	if _, err := os.Stat(codePath); err != nil {
		t.Fatalf("code file not found: %v", err)
	}

	// Verify AGENT.md was created
	agentPath := filepath.Join(home, "buttons", "test", "AGENT.md")
	if _, err := os.Stat(agentPath); err != nil {
		t.Fatalf("AGENT.md not found: %v", err)
	}

	// Verify pressed/ directory was created
	pressedDir := filepath.Join(home, "buttons", "test", "pressed")
	if _, err := os.Stat(pressedDir); err != nil {
		t.Fatalf("pressed/ directory not found: %v", err)
	}

	// Verify JSON content
	data, _ := os.ReadFile(specPath)
	var saved Button
	json.Unmarshal(data, &saved)
	if saved.SchemaVersion != 2 {
		t.Errorf("saved schema_version = %d, want 2", saved.SchemaVersion)
	}
	if len(saved.Args) != 1 || saved.Args[0].Name != "url" {
		t.Errorf("saved args = %+v, want [{url string true}]", saved.Args)
	}
}

func TestService_Create_ScaffoldNoSource(t *testing.T) {
	home := setupTestEnv(t)
	svc := NewService()

	// No FilePath, Code, URL, or Prompt — should scaffold a shell button.
	btn, err := svc.Create(CreateOpts{Name: "scaffolded", TimeoutSeconds: 60})
	if err != nil {
		t.Fatalf("scaffold create failed: %v", err)
	}
	if btn.Runtime != "shell" {
		t.Errorf("runtime = %q, want shell", btn.Runtime)
	}

	// main.sh should exist with the shebang placeholder.
	codePath := filepath.Join(home, "buttons", "scaffolded", "main.sh")
	data, err := os.ReadFile(codePath)
	if err != nil {
		t.Fatalf("scaffolded main.sh not found: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("scaffolded main.sh is empty, expected shebang + TODO")
	}
}

func TestService_Create_CustomTimeout(t *testing.T) {
	home := setupTestEnv(t)
	script := createTestScript(t, home)
	svc := NewService()

	btn, err := svc.Create(CreateOpts{
		Name:           "test",
		FilePath:       script,
		TimeoutSeconds: 30,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if btn.TimeoutSeconds != 30 {
		t.Errorf("timeout = %d, want 30", btn.TimeoutSeconds)
	}
}

func TestService_Create_Duplicate(t *testing.T) {
	home := setupTestEnv(t)
	script := createTestScript(t, home)
	svc := NewService()

	svc.Create(CreateOpts{Name: "test", FilePath: script, TimeoutSeconds: 60})
	_, err := svc.Create(CreateOpts{Name: "test", FilePath: script, TimeoutSeconds: 60})
	if err == nil {
		t.Fatal("expected error for duplicate name")
	}
	se, ok := err.(*ServiceError)
	if !ok || se.Code != "VALIDATION_ERROR" {
		t.Errorf("expected VALIDATION_ERROR, got %v", err)
	}
}

func TestService_Create_FileNotExist(t *testing.T) {
	setupTestEnv(t)
	svc := NewService()

	_, err := svc.Create(CreateOpts{Name: "test", FilePath: "/nonexistent.sh", TimeoutSeconds: 60})
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
	se, ok := err.(*ServiceError)
	if !ok || se.Code != "VALIDATION_ERROR" {
		t.Errorf("expected VALIDATION_ERROR, got %v", err)
	}
}

func TestService_List_Empty(t *testing.T) {
	setupTestEnv(t)
	svc := NewService()

	buttons, err := svc.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(buttons) != 0 {
		t.Errorf("expected empty list, got %d", len(buttons))
	}
}

func TestService_List_Sorted(t *testing.T) {
	home := setupTestEnv(t)
	script := createTestScript(t, home)
	svc := NewService()

	svc.Create(CreateOpts{Name: "zebra", FilePath: script, TimeoutSeconds: 60})
	svc.Create(CreateOpts{Name: "alpha", FilePath: script, TimeoutSeconds: 60})

	buttons, err := svc.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(buttons) != 2 {
		t.Fatalf("expected 2 buttons, got %d", len(buttons))
	}
	if buttons[0].Name != "alpha" || buttons[1].Name != "zebra" {
		t.Errorf("expected sorted [alpha, zebra], got [%s, %s]", buttons[0].Name, buttons[1].Name)
	}
}

func TestService_Get_NotFound(t *testing.T) {
	setupTestEnv(t)
	svc := NewService()

	_, err := svc.Get("ghost")
	if err == nil {
		t.Fatal("expected error")
	}
	se, ok := err.(*ServiceError)
	if !ok || se.Code != "NOT_FOUND" {
		t.Errorf("expected NOT_FOUND, got %v", err)
	}
}

func TestService_Remove(t *testing.T) {
	home := setupTestEnv(t)
	script := createTestScript(t, home)
	svc := NewService()

	svc.Create(CreateOpts{Name: "test", FilePath: script, TimeoutSeconds: 60})
	if err := svc.Remove("test"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := svc.Get("test")
	if err == nil {
		t.Fatal("expected NOT_FOUND after remove")
	}
}

func TestService_Remove_NotFound(t *testing.T) {
	setupTestEnv(t)
	svc := NewService()

	err := svc.Remove("ghost")
	if err == nil {
		t.Fatal("expected error")
	}
	se, ok := err.(*ServiceError)
	if !ok || se.Code != "NOT_FOUND" {
		t.Errorf("expected NOT_FOUND, got %v", err)
	}
}
