package trigger

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/history"
)

func writeButton(t *testing.T, home, name string) {
	t.Helper()
	dir := filepath.Join(home, "buttons", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	spec := `{"schema_version":1,"name":"` + name + `","runtime":"shell","env":{},"timeout_seconds":30,"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, "button.json"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.sh"), []byte("#!/bin/sh\necho fired\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestAddListRemove(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	writeButton(t, home, "job")
	svc := button.NewService()

	created, err := Add(svc, "job", button.Trigger{Kind: KindCron, Schedule: "0 */6 * * *"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if created.ID == "" {
		t.Fatal("trigger should get an id")
	}

	trs, err := List(svc, "job")
	if err != nil || len(trs) != 1 {
		t.Fatalf("list: %v len=%d", err, len(trs))
	}

	all, _ := ListAll(svc)
	if len(all) != 1 || all[0].Button != "job" {
		t.Fatalf("listall: %+v", all)
	}

	if err := Remove(svc, "job", created.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if trs, _ := List(svc, "job"); len(trs) != 0 {
		t.Fatalf("expected 0 triggers after remove, got %d", len(trs))
	}
	if err := Remove(svc, "job", "nope"); err == nil {
		t.Fatal("removing unknown id should error")
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name string
		tr   button.Trigger
		ok   bool
	}{
		{"cron ok", button.Trigger{Kind: KindCron, Schedule: "*/5 * * * *"}, true},
		{"cron no schedule", button.Trigger{Kind: KindCron}, false},
		{"cron bad expr", button.Trigger{Kind: KindCron, Schedule: "not a cron"}, false},
		{"watch ok", button.Trigger{Kind: KindWatch, Path: "./x.json"}, true},
		{"watch no path", button.Trigger{Kind: KindWatch}, false},
		{"webhook ok", button.Trigger{Kind: KindWebhook, Path: "/hooks/x"}, true},
		{"webhook no slash", button.Trigger{Kind: KindWebhook, Path: "hooks/x"}, false},
		{"unknown kind", button.Trigger{Kind: "nope"}, false},
	}
	for _, c := range cases {
		err := Validate(c.tr)
		if (err == nil) != c.ok {
			t.Errorf("%s: ok=%v err=%v", c.name, c.ok, err)
		}
	}
}

func TestWebhookHandlerTokenGate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	writeButton(t, home, "deploy")

	eng := NewEngine(button.NewService(), nil)
	h := eng.WebhookHandler(WebhookRoute{Path: "/hooks/deploy", Button: "deploy", Token: "s3cr3t"})

	// Wrong/missing token → 401.
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest("POST", "/hooks/deploy", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing token should 401, got %d", rec.Code)
	}

	// GET → 405.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/hooks/deploy", nil)
	req.Header.Set("X-Buttons-Token", "s3cr3t")
	h(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET should 405, got %d", rec.Code)
	}

	// Correct token → 202.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/hooks/deploy", nil)
	req.Header.Set("X-Buttons-Token", "s3cr3t")
	h(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("valid token should 202, got %d", rec.Code)
	}
}

func TestEngineWatchFires(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	writeButton(t, home, "reindex")
	svc := button.NewService()

	watched := filepath.Join(t.TempDir(), "data.json")
	if err := os.WriteFile(watched, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(svc, "reindex", button.Trigger{Kind: KindWatch, Path: watched}); err != nil {
		t.Fatalf("add watch: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	eng := NewEngine(svc, nil)
	if _, err := eng.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { cancel(); eng.Stop() }()

	// Let the watcher prime, then change the file.
	time.Sleep(700 * time.Millisecond)
	if err := os.WriteFile(watched, []byte("v2-changed"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Wait up to ~4s for the press to be recorded.
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		runs, _ := history.List("reindex", 1)
		if len(runs) >= 1 {
			return // fired
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatal("watch trigger did not fire a press within timeout")
}

func TestEngineStartCountsKinds(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	writeButton(t, home, "multi")
	svc := button.NewService()
	_, _ = Add(svc, "multi", button.Trigger{Kind: KindWebhook, Path: "/hooks/a"})
	_, _ = Add(svc, "multi", button.Trigger{Kind: KindCron, Schedule: "* * * * *"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eng := NewEngine(svc, nil)
	routes, err := eng.Start(ctx)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer eng.Stop()
	if len(routes) != 1 || routes[0].Path != "/hooks/a" {
		t.Fatalf("expected 1 webhook route /hooks/a, got %+v", routes)
	}
}
