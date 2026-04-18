package tui

import (
	"testing"

	"github.com/autonoco/buttons/internal/button"
)

func newTestDetailModel(btn *button.Button, codePath string) DetailModel {
	return *NewDetail(btn, nil, "", codePath)
}

func TestDetailUpdate_EscQuits(t *testing.T) {
	m := newTestDetailModel(&button.Button{Name: "weather", Runtime: "shell"}, "")
	_, cmd := m.Update(keyPress("esc"))
	if cmd == nil {
		t.Error("esc should emit tea.Quit; got nil")
	}
}

func TestDetailUpdate_EnterQuits(t *testing.T) {
	m := newTestDetailModel(&button.Button{Name: "weather", Runtime: "shell"}, "")
	_, cmd := m.Update(keyPress("enter"))
	if cmd == nil {
		t.Error("enter should emit tea.Quit; got nil")
	}
}

func TestDetailUpdate_EditRequestsEditorWhenCodePath(t *testing.T) {
	m := newTestDetailModel(&button.Button{Name: "weather", Runtime: "shell"}, "/tmp/main.sh")
	next, _ := m.Update(keyPress("e"))
	nm := next.(DetailModel)
	if !nm.EditRequested() {
		t.Error("e should set editRequested when codePath is non-empty")
	}
}

func TestDetailUpdate_EditNoOpWithoutCodePath(t *testing.T) {
	// HTTP button: codePath is empty → e should be a no-op so the
	// user doesn't get an $EDITOR session on nothing.
	m := newTestDetailModel(&button.Button{Name: "weather", Runtime: "http", URL: "https://example.com"}, "")
	next, cmd := m.Update(keyPress("e"))
	nm := next.(DetailModel)
	if nm.EditRequested() {
		t.Error("e should NOT set editRequested when codePath is empty")
	}
	if cmd != nil {
		t.Error("e with empty codePath should not emit a command")
	}
}

func TestDetailView_SnapshotShapes(t *testing.T) {
	btn := &button.Button{
		Name:           "deploy",
		Runtime:        "shell",
		TimeoutSeconds: 300,
		Args: []button.ArgDef{
			{Name: "env", Type: "string", Required: true},
		},
	}
	m := newTestDetailModel(btn, "/tmp/main.sh")
	m.width, m.height = 100, 40
	view := m.View()
	out := view.Content
	// Smoke checks: key sections appear in order. Proper snapshot
	// tests come with Group F1.
	wants := []string{
		"deploy", // header
		"runtime",
		"shell",
		"timeout",
		"args",
		"env",
		"usage",
		"buttons press deploy --arg env=<string>",
	}
	for _, w := range wants {
		if !containsLiteral(out, w) {
			t.Errorf("view missing substring %q; got:\n%s", w, out)
		}
	}
}

func containsLiteral(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

// indexOf is a local strings.Contains/Index replacement so the test
// doesn't need to import strings alongside the package-under-test.
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
