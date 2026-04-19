package drawer

import (
	"os"
	"path/filepath"
	"testing"
)

// newTestHome sets up an isolated BUTTONS_HOME for one test.
func newTestHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("BUTTONS_HOME", dir)
	for _, sub := range []string{"buttons", "drawers"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestService_CreateListGet(t *testing.T) {
	newTestHome(t)
	svc := NewService()

	d, err := svc.Create("test-flow", "my flow", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if d.Name != "test-flow" {
		t.Errorf("name: got %q", d.Name)
	}
	if d.SchemaVersion != SchemaVersion {
		t.Errorf("schema: got %d", d.SchemaVersion)
	}

	list, err := svc.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].Name != "test-flow" {
		t.Errorf("List: got %+v", list)
	}

	got, err := svc.Get("test-flow")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Description != "my flow" {
		t.Errorf("description round-trip: got %q", got.Description)
	}
}

func TestService_DuplicateCreate_Errors(t *testing.T) {
	newTestHome(t)
	svc := NewService()
	if _, err := svc.Create("dup", "", nil); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Create("dup", "", nil)
	if err == nil {
		t.Fatal("expected error for duplicate, got nil")
	}
	se, ok := err.(*ServiceError)
	if !ok || se.Code != "VALIDATION_ERROR" {
		t.Errorf("expected VALIDATION_ERROR, got %v", err)
	}
}

func TestService_Remove(t *testing.T) {
	newTestHome(t)
	svc := NewService()
	if _, err := svc.Create("rm-me", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.Remove("rm-me"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := svc.Get("rm-me"); err == nil {
		t.Error("expected NOT_FOUND after remove")
	}
}

func TestService_SetArg_PersistsToStep(t *testing.T) {
	newTestHome(t)
	svc := NewService()
	d, err := svc.Create("wire", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Hand-append a step (AddSteps requires a real button, which we
	// don't want to create in a service-layer test).
	d.Steps = []Step{{ID: "s1", Kind: "button", Button: "any", Args: map[string]any{}}}
	if err := svc.save(d); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.SetArg("wire", "s1", "name", "${inputs.x}"); err != nil {
		t.Fatalf("SetArg: %v", err)
	}
	got, _ := svc.Get("wire")
	if got.Steps[0].Args["name"] != "${inputs.x}" {
		t.Errorf("Args: got %v", got.Steps[0].Args)
	}
}

func TestService_SetArg_UnknownStep_Errors(t *testing.T) {
	newTestHome(t)
	svc := NewService()
	if _, err := svc.Create("wire", "", nil); err != nil {
		t.Fatal(err)
	}
	_, err := svc.SetArg("wire", "nope", "name", "value")
	if err == nil {
		t.Fatal("expected STEP_NOT_FOUND")
	}
	se, ok := err.(*ServiceError)
	if !ok || se.Code != "STEP_NOT_FOUND" {
		t.Errorf("got %v", err)
	}
}

func TestService_ReservedName_Rejected(t *testing.T) {
	newTestHome(t)
	svc := NewService()
	_, err := svc.Create("list", "", nil) // reserved subcommand name
	if err == nil {
		t.Fatal("expected reserved-name rejection")
	}
}
