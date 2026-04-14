package agentskill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAgentMD(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".buttons"), 0700); err != nil {
		t.Fatal(err)
	}

	path, err := WriteAgentMD(dir)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, ".buttons", "AGENT.md") {
		t.Errorf("unexpected path: %s", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "This folder is managed by") {
		t.Error("AGENT.md missing expected heading content")
	}
}

func TestInstall_CreatesAgentsMD(t *testing.T) {
	dir := t.TempDir()

	results, err := Install(InstallOpts{ProjectRoot: dir, TargetIDs: []string{"agents-md"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Action != "created" {
		t.Fatalf("expected 1 created result, got %+v", results)
	}

	data, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, markerStart) || !strings.Contains(s, markerEnd) {
		t.Error("expected markers in new AGENTS.md")
	}
	if !strings.Contains(s, "## Buttons") {
		t.Error("expected Buttons heading in new AGENTS.md")
	}
}

func TestInstall_AppendsToExistingFile(t *testing.T) {
	dir := t.TempDir()
	existing := "# My existing rules\n\nSome other instructions here.\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(existing), 0600); err != nil {
		t.Fatal(err)
	}

	results, err := Install(InstallOpts{ProjectRoot: dir, TargetIDs: []string{"claude"}})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Action != "appended" {
		t.Errorf("expected appended, got %s", results[0].Action)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	s := string(data)
	if !strings.Contains(s, "My existing rules") {
		t.Error("original content was clobbered")
	}
	if !strings.Contains(s, "## Buttons") {
		t.Error("Buttons section was not appended")
	}
	// The existing content should come before the Buttons section.
	if strings.Index(s, "My existing rules") >= strings.Index(s, "## Buttons") {
		t.Error("existing content did not come before the appended section")
	}
}

func TestInstall_UpdatesExistingMarkers(t *testing.T) {
	dir := t.TempDir()
	// Simulate a previous Install: user content above, stale Buttons
	// section below wrapped in markers.
	prior := "# Notes\n\nUser stuff.\n\n" +
		markerStart + "\n## Buttons\n\nOLD STALE CONTENT\n" + markerEnd + "\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(prior), 0600); err != nil {
		t.Fatal(err)
	}

	results, err := Install(InstallOpts{ProjectRoot: dir, TargetIDs: []string{"claude"}})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Action != "updated" {
		t.Errorf("expected updated, got %s", results[0].Action)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	s := string(data)
	if strings.Contains(s, "OLD STALE CONTENT") {
		t.Error("stale content was not replaced")
	}
	if !strings.Contains(s, "User stuff.") {
		t.Error("user content outside markers was clobbered")
	}
	// There should be exactly one markerStart and one markerEnd — not
	// duplicated by re-running.
	if strings.Count(s, markerStart) != 1 {
		t.Errorf("expected exactly 1 markerStart, got %d", strings.Count(s, markerStart))
	}
	if strings.Count(s, markerEnd) != 1 {
		t.Errorf("expected exactly 1 markerEnd, got %d", strings.Count(s, markerEnd))
	}
}

func TestInstall_CursorWritesFullFile(t *testing.T) {
	dir := t.TempDir()

	results, err := Install(InstallOpts{ProjectRoot: dir, TargetIDs: []string{"cursor"}})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Action != "created" {
		t.Errorf("expected created, got %s", results[0].Action)
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".cursor", "rules", "buttons.mdc"))
	s := string(data)
	if !strings.HasPrefix(s, "---\n") {
		t.Error("cursor rule should start with MDC frontmatter")
	}
	if !strings.Contains(s, "## Buttons") {
		t.Error("cursor rule missing Buttons section")
	}
}

func TestInstall_CursorOverwritesOnRerun(t *testing.T) {
	dir := t.TempDir()
	ruleDir := filepath.Join(dir, ".cursor", "rules")
	if err := os.MkdirAll(ruleDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ruleDir, "buttons.mdc"), []byte("old content"), 0600); err != nil {
		t.Fatal(err)
	}

	results, err := Install(InstallOpts{ProjectRoot: dir, TargetIDs: []string{"cursor"}})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Action != "updated" {
		t.Errorf("expected updated, got %s", results[0].Action)
	}

	data, _ := os.ReadFile(filepath.Join(ruleDir, "buttons.mdc"))
	if strings.Contains(string(data), "old content") {
		t.Error("old content should have been replaced")
	}
}

func TestInstall_UnknownTarget(t *testing.T) {
	dir := t.TempDir()
	_, err := Install(InstallOpts{ProjectRoot: dir, TargetIDs: []string{"nonexistent"}})
	if err == nil {
		t.Fatal("expected error for unknown target")
	}
	if !strings.Contains(err.Error(), "unknown agent target") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestTargetByID(t *testing.T) {
	if _, ok := TargetByID("cursor"); !ok {
		t.Error("cursor should be a known target")
	}
	if _, ok := TargetByID("not-a-thing"); ok {
		t.Error("unknown id should return false")
	}
}
