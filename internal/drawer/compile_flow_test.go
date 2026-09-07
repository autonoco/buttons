package drawer

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestCompileFlowPipelineShape(t *testing.T) {
	d := decodeDrawerJSON(t, validFlowDrawerJSON())
	compiled, err := CompileFlow(d, CompileOptions{Provider: "local"})
	if err != nil {
		t.Fatalf("CompileFlow: %v", err)
	}
	if compiled.DrawerKind != DrawerKindAction {
		t.Fatalf("DrawerKind = %q, want action", compiled.DrawerKind)
	}
	if compiled.Name != d.Name {
		t.Fatalf("Name = %q, want %q", compiled.Name, d.Name)
	}
	if len(compiled.Steps) != 3 {
		t.Fatalf("steps = %d, want 3", len(compiled.Steps))
	}
	if compiled.Steps[0].ID != "provider-list" || compiled.Steps[0].Button != "flow-local-provider-list" {
		t.Fatalf("provider-list step = %#v", compiled.Steps[0])
	}
	fe := compiled.Steps[1]
	if fe.Kind != "for_each" || fe.Over != "provider-list.output.items" || fe.OnItemFailure != "continue" {
		t.Fatalf("for_each = %#v", fe)
	}
	if len(fe.Steps) != 1 || fe.Steps[0].Kind != "switch" {
		t.Fatalf("for_each body = %#v", fe.Steps)
	}
	if compiled.Steps[2].Button != "flow-local-ensure-trigger" {
		t.Fatalf("ensure-trigger = %#v", compiled.Steps[2])
	}

	// Disk entity must still marshal without steps.
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	if _, ok := obj["steps"]; ok {
		t.Fatalf("flow drawer marshal leaked steps: %s", raw)
	}
}

func TestCompileFlowGitHubButtons(t *testing.T) {
	d := decodeDrawerJSON(t, validFlowDrawerJSON())
	compiled, err := CompileFlow(d, CompileOptions{Provider: "github"})
	if err != nil {
		t.Fatalf("CompileFlow: %v", err)
	}
	if compiled.Steps[0].Button != "flow-github-provider-list" {
		t.Fatalf("github provider button = %q", compiled.Steps[0].Button)
	}
}

func TestCompileFlowValidationFailure(t *testing.T) {
	d := decodeDrawerJSON(t, validFlowDrawerJSON())
	d.Flow.InitialStage = "missing"
	_, err := CompileFlow(d, CompileOptions{})
	if err == nil {
		t.Fatal("expected validation error")
	}
	var se *ServiceError
	if !errors.As(err, &se) || se.Code != "VALIDATION_ERROR" {
		t.Fatalf("err = %#v", err)
	}
	if !strings.Contains(se.Message, "initial stage") {
		t.Fatalf("message = %q", se.Message)
	}
}

func TestPrepareForExecutePassthrough(t *testing.T) {
	action := &Drawer{Name: "x", DrawerKind: DrawerKindAction, Steps: []Step{{ID: "a", Button: "b"}}}
	got, err := PrepareForExecute(action)
	if err != nil {
		t.Fatal(err)
	}
	if got != action {
		t.Fatal("action drawer should pass through unchanged")
	}
}
