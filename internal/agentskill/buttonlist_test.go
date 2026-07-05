package agentskill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTargetsIncludeOpenClawAndHermes(t *testing.T) {
	for _, id := range []string{"openclaw", "hermes"} {
		tg, ok := TargetByID(id)
		if !ok {
			t.Fatalf("target %q not registered", id)
		}
		if tg.Path != "AGENTS.md" {
			t.Errorf("target %q: Path = %q, want AGENTS.md", id, tg.Path)
		}
		if tg.Format != formatMarkdownSection {
			t.Errorf("target %q: not a markdown-section target", id)
		}
	}
}

func TestRenderButtonList(t *testing.T) {
	got := RenderButtonList([]ButtonEntry{
		{Name: "deploy", Description: "ship it"},
		{Name: "bare", Description: ""},
	})
	if !strings.Contains(got, "- `buttons press deploy` — ship it") {
		t.Errorf("missing deploy line:\n%s", got)
	}
	if !strings.Contains(got, "- `buttons press bare`") || strings.Contains(got, "bare` —") {
		t.Errorf("bare button should render without an em-dash:\n%s", got)
	}
}

func TestRenderButtonListCapsAndOverflows(t *testing.T) {
	// Caller passes most-pressed-first; the first ButtonListCap render in full,
	// the rest collapse to a comma-separated name line.
	entries := make([]ButtonEntry, 0, ButtonListCap+2)
	for i := 0; i < ButtonListCap; i++ {
		entries = append(entries, ButtonEntry{Name: fmt.Sprintf("hot%02d", i), Description: "d"})
	}
	entries = append(entries, ButtonEntry{Name: "cold1", Description: "unused"}, ButtonEntry{Name: "cold2", Description: "unused"})

	got := RenderButtonList(entries)
	if !strings.Contains(got, "- `buttons press hot00` — d") {
		t.Errorf("top button should render in full:\n%s", got)
	}
	if strings.Contains(got, "- `buttons press cold1`") {
		t.Errorf("overflow button should not get a full press line:\n%s", got)
	}
	if !strings.Contains(got, "Plus 2 more: cold1, cold2 — run `buttons list`") {
		t.Errorf("expected overflow summary line:\n%s", got)
	}
	if strings.Contains(got, "unused") {
		t.Errorf("overflow descriptions should be dropped:\n%s", got)
	}
}

func TestWriteButtonListCreateThenUpdateIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	if _, err := WriteButtonList(dir, []ButtonEntry{{Name: "a", Description: "first"}}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "buttons press a") {
		t.Fatalf("expected button a in AGENTS.md:\n%s", b)
	}
	if _, err := WriteButtonList(dir, []ButtonEntry{{Name: "a", Description: "first"}, {Name: "b", Description: "second"}}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	s := string(b)
	if strings.Count(s, listMarkerStart) != 1 || strings.Count(s, listMarkerEnd) != 1 {
		t.Errorf("expected one LIST block, got start=%d end=%d", strings.Count(s, listMarkerStart), strings.Count(s, listMarkerEnd))
	}
	if !strings.Contains(s, "buttons press b") {
		t.Errorf("expected button b after update:\n%s", s)
	}
	if strings.Count(s, "buttons press a") != 1 {
		t.Errorf("button a should appear once, got %d", strings.Count(s, "buttons press a"))
	}
}

func TestWriteButtonListInsertsAfterButtonsSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	seed := "# My project\n\n" + markerStart + "\n" + Body + "\n" + markerEnd + "\n\n## Notes\nmine\n"
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteButtonList(dir, []ButtonEntry{{Name: "x", Description: "ex"}}); err != nil {
		t.Fatal(err)
	}
	s := string(mustRead(t, path))
	if !strings.Contains(s, markerStart) || !strings.Contains(s, "## Notes") {
		t.Errorf("clobbered existing content:\n%s", s)
	}
	if strings.Index(s, listMarkerStart) < strings.Index(s, markerEnd) {
		t.Errorf("LIST block should sit after the BUTTONS section:\n%s", s)
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
