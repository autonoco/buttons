package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordLifecycleEventAppends(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)

	when := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	if err := RecordLifecycleEventAt(LifecycleEvent{
		Action:      "add",
		OccurredAt:  when,
		PackageName: "@autono/hello",
		Requested:   "latest",
		Installed:   []string{"hello"},
		Dependencies: []LifecycleDependency{
			{Name: "@autono/z", Version: "1.0.0"},
			{Name: "@autono/a", Version: "1.0.0"},
		},
	}); err != nil {
		t.Fatalf("record add event: %v", err)
	}
	if err := RecordLifecycleEventAt(LifecycleEvent{
		Action:     "install",
		OccurredAt: when.Add(time.Minute),
		Installed:  []string{"hello"},
	}); err != nil {
		t.Fatalf("record install event: %v", err)
	}

	log, err := LoadLifecycleLog()
	if err != nil {
		t.Fatalf("load lifecycle log: %v", err)
	}
	if log.SchemaVersion != lifecycleSchemaVersion {
		t.Fatalf("schema_version = %d, want %d", log.SchemaVersion, lifecycleSchemaVersion)
	}
	if len(log.Events) != 2 {
		t.Fatalf("events = %d, want 2", len(log.Events))
	}
	if got := log.Events[0].Action; got != "add" {
		t.Fatalf("first action = %q, want add", got)
	}
	if got := log.Events[0].Dependencies[0].Name; got != "@autono/a" {
		t.Fatalf("dependencies not sorted, first = %q", got)
	}
	if got := log.Events[1].Action; got != "install" {
		t.Fatalf("second action = %q, want install", got)
	}

	info, err := os.Stat(filepath.Join(home, "history.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("history.json mode = %v, want 0600", got)
	}
}

func TestRecordLifecycleEventRequiresAction(t *testing.T) {
	t.Setenv("BUTTONS_HOME", t.TempDir())
	if err := RecordLifecycleEventAt(LifecycleEvent{}); err == nil {
		t.Fatal("expected missing action to fail")
	}
}
