package importer

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/autonoco/buttons/internal/button"
)

func TestPlanCodeInfersRuntime(t *testing.T) {
	dir := t.TempDir()
	py := filepath.Join(dir, "deploy.py")
	if err := os.WriteFile(py, []byte("print('hi')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanCode(py, "")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Items) != 1 || plan.Items[0].Runtime != "python" || plan.Items[0].Name != "deploy" {
		t.Fatalf("unexpected plan: %+v", plan.Items)
	}

	// Shebang inference for an extensionless file.
	sh := filepath.Join(dir, "runme")
	if err := os.WriteFile(sh, []byte("#!/usr/bin/env node\nconsole.log(1)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, _ = PlanCode(sh, "tool")
	if plan.Items[0].Runtime != "node" || plan.Items[0].Name != "tool" {
		t.Fatalf("shebang/override failed: %+v", plan.Items[0])
	}
}

func TestPlanCodeApplyCreatesFunctionalButton(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	dir := t.TempDir()
	f := filepath.Join(dir, "hello.sh")
	if err := os.WriteFile(f, []byte("#!/bin/sh\necho hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanCode(f, "")
	if err != nil {
		t.Fatal(err)
	}
	res := Apply(button.NewService(), plan)
	if len(res.Created) != 1 || res.Errors != nil {
		t.Fatalf("apply: created=%v errors=%v", res.Created, res.Errors)
	}
	// The created button is real and carries the imported code.
	if _, err := button.NewService().Get("hello"); err != nil {
		t.Fatalf("imported button missing: %v", err)
	}
	code, _ := os.ReadFile(filepath.Join(home, "buttons", "hello", "main.sh"))
	if string(code) != "#!/bin/sh\necho hi\n" {
		t.Fatalf("code not imported: %q", code)
	}
}

func TestPlanSkillScripts(t *testing.T) {
	skill := t.TempDir()
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("# Deploy Tools\n\nDeploy helpers.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scripts := filepath.Join(skill, "scripts")
	if err := os.MkdirAll(scripts, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(scripts, "build.sh"), []byte("#!/bin/sh\necho build\n"), 0o644)
	_ = os.WriteFile(filepath.Join(scripts, "ship.py"), []byte("print('ship')\n"), 0o644)

	plan, err := PlanSkill(skill)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Items) != 2 {
		t.Fatalf("want 2 buttons from scripts, got %d", len(plan.Items))
	}
	byName := map[string]Planned{}
	for _, it := range plan.Items {
		byName[it.Name] = it
		if it.Description != "Deploy Tools" {
			t.Fatalf("description should come from SKILL.md heading, got %q", it.Description)
		}
	}
	base := filepath.Base(skill)
	if byName[base+"-ship"].Runtime != "python" {
		t.Fatalf("ship.py should be python: %+v", byName)
	}
}

func TestPlanSkillPromptFallback(t *testing.T) {
	skill := filepath.Join(t.TempDir(), "writer")
	if err := os.MkdirAll(skill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("# Writer\n\nDraft prose.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanSkill(skill)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Items) != 1 || plan.Items[0].Runtime != "prompt" || plan.Items[0].Name != "writer" {
		t.Fatalf("expected single prompt button, got %+v", plan.Items)
	}
}

func TestPlanURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"weather","runtime":"http","url":"https://api.example.com/w","method":"GET"}`))
	}))
	defer srv.Close()

	plan, err := PlanURL(srv.URL, "")
	if err != nil {
		t.Fatalf("plan url: %v", err)
	}
	if plan.Items[0].Name != "weather" || plan.Items[0].Runtime != "http" {
		t.Fatalf("unexpected url plan: %+v", plan.Items[0])
	}

	// A non-HTTP spec (no url) is rejected with guidance.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"local","runtime":"shell"}`))
	}))
	defer srv2.Close()
	if _, err := PlanURL(srv2.URL, ""); err == nil {
		t.Fatal("non-http spec should be rejected")
	}
}
