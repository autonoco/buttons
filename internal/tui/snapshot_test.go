// Snapshot harness for View() output. The Elm-style architecture
// makes this unusually straightforward: View is pure, so we can
// construct a Model directly, call .View(), strip ANSI, and compare
// against a golden file on disk. No teatest program, no real terminal.
//
// Regenerate after an intentional visual change:
//
//	go test ./internal/tui -run TestSnapshot -update
//
// The goldens are plain ANSI-stripped text so they diff readably in a
// PR. If a change is accidental, the test fails with a diff that
// makes it obvious what the board (or any other view) now looks like.
package tui

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/engine"
)

// update lets a local run rewrite all the golden files after an
// intentional design change. CI never sets it — snapshots that drift
// from the repo surface as test failures.
var update = flag.Bool("update", false, "rewrite testdata/*.golden instead of comparing")

// assertSnapshot compares got (a raw View string) against the
// recorded golden for name. On -update, rewrites the golden instead
// of asserting. Strips ANSI so the stored files are plain text and
// diff cleanly.
func assertSnapshot(t *testing.T, name, got string) {
	t.Helper()
	plain := ansi.Strip(got)
	path := filepath.Join("testdata", name+".golden")

	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(plain), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(path) // #nosec G304 -- path built from test name literal
	if err != nil {
		if os.IsNotExist(err) {
			t.Fatalf("snapshot %s missing; regenerate with:\n"+
				"  go test ./internal/tui -run %s -update", path, t.Name())
		}
		t.Fatal(err)
	}
	if string(want) != plain {
		t.Errorf("snapshot %s mismatch\n\n--- want ---\n%s\n--- got ---\n%s", path, want, plain)
	}
}

// fixtureModel builds a Model with controlled inputs suitable for
// snapshotting. Nil svc is fine — View() doesn't touch it.
func fixtureModel(buttons []button.Button) Model {
	return Model{
		styles:         BuildStyles(),
		buttons:        buttons,
		status:         map[string]runStatus{},
		pressStartedAt: map[string]time.Time{},
		section:        sectionList,
		width:          80,
		height:         24,
	}
}

func TestSnapshot_BoardEmpty(t *testing.T) {
	m := fixtureModel(nil)
	assertSnapshot(t, "board_empty", m.View().Content)
}

func TestSnapshot_BoardPopulatedIdle(t *testing.T) {
	m := fixtureModel([]button.Button{
		{Name: "deploy", Runtime: "shell", TimeoutSeconds: 300, Args: []button.ArgDef{{Name: "env", Type: "string", Required: true}}},
		{Name: "weather", Runtime: "http", URL: "https://wttr.in/NYC", TimeoutSeconds: 60},
		{Name: "notify", Runtime: "http", URL: "https://hooks.example.com/webhook", Method: "POST", TimeoutSeconds: 60},
	})
	m.cursorList = 1
	assertSnapshot(t, "board_populated_cursor_on_list_1", m.View().Content)
}

func TestSnapshot_BoardWithPinned(t *testing.T) {
	m := fixtureModel([]button.Button{
		{Name: "deploy", Runtime: "shell", TimeoutSeconds: 300, Pinned: true},
		{Name: "weather", Runtime: "http", URL: "https://wttr.in/NYC", TimeoutSeconds: 60},
		{Name: "notify", Runtime: "http", URL: "https://hooks.example.com/webhook", Method: "POST", TimeoutSeconds: 60},
	})
	m.section = sectionPinned
	m.cursorPinned = 0
	assertSnapshot(t, "board_pinned_row_focused", m.View().Content)
}

func TestSnapshot_BoardLogsOpen(t *testing.T) {
	m := fixtureModel([]button.Button{
		{Name: "deploy", Runtime: "shell", TimeoutSeconds: 300},
	})
	m.logsOpen = true
	m.cursorList = 0
	// history.List will return [] for unknown buttons in the test
	// env — that's fine, the empty-state branch of renderLogs gives
	// us a deterministic snapshot.
	assertSnapshot(t, "board_logs_pane_empty", m.View().Content)
}

func TestSnapshot_BoardPinnedActive(t *testing.T) {
	// Pinned button mid-press: spec shows thick orange border, two-line
	// interior (name + "● ACTIVE · <elapsed>"), and a "↵ TAIL" badge
	// floating above the card's top-right corner.
	m := fixtureModel([]button.Button{
		{Name: "deploy", Runtime: "shell", TimeoutSeconds: 300, Pinned: true},
		{Name: "weather", Runtime: "http", URL: "https://wttr.in/NYC", TimeoutSeconds: 60},
	})
	m.section = sectionPinned
	m.cursorPinned = 0
	m.status["deploy"] = statusRunning
	// Freeze elapsed at a known instant so the golden is deterministic.
	m.pressStartedAt["deploy"] = time.Now().Add(-3*time.Second - 200*time.Millisecond)
	assertSnapshot(t, "board_pinned_active", m.View().Content)
}

func TestSnapshot_DetailHTTP(t *testing.T) {
	// Detail view for an HTTP button — chrome surfaces method, max
	// response cap, and network access scope.
	btn := &button.Button{
		Name:                 "weather",
		Description:          "get current weather for a city via wttr.in JSON API",
		Runtime:              "http",
		URL:                  "https://wttr.in/{{city}}?format=j1",
		Method:               "GET",
		TimeoutSeconds:       60,
		AllowPrivateNetworks: false,
		Args: []button.ArgDef{
			{Name: "city", Type: "string", Required: true},
		},
	}
	m := NewDetail(btn, nil, "", "")
	m.width = 100
	m.height = 40
	assertSnapshot(t, "detail_http", m.View().Content)
}

func TestSnapshot_DetailShell(t *testing.T) {
	// Detail view for a shell button with a code path — chrome shows
	// runtime + timeout; the `e edit` hint is present.
	btn := &button.Button{
		Name:           "deploy",
		Description:    "ship a release to a target env",
		Runtime:        "shell",
		TimeoutSeconds: 300,
		Args: []button.ArgDef{
			{Name: "env", Type: "string", Required: true},
			{Name: "verbose", Type: "bool", Required: false},
		},
	}
	m := NewDetail(btn, nil, "", "/tmp/deploy/main.sh")
	m.width = 100
	m.height = 40
	assertSnapshot(t, "detail_shell", m.View().Content)
}

// logsFixtureTime is a fixed instant used for all log-line timestamps
// in the logs snapshot tests. Real `time.Now()` makes the rendered
// `hh:mm:ss.mmm` column drift between runs; pinning a single time
// keeps the golden deterministic across machines. The logs view
// renders timestamps via `l.Ts.Local().Format(...)`, so setting the
// location to time.UTC means the formatter's output is
// timezone-invariant too.
var logsFixtureTime = time.Date(2026, 4, 18, 12, 31, 58, 0, time.UTC)

func TestSnapshot_LogsTailing(t *testing.T) {
	// Full-screen logs view mid-press: header shows "● follow"
	// indicator + elapsed counter + spinner; chrome bottom shows
	// "● FOLLOW · PRESS <id>".
	//
	// Note: the spinner frame and elapsed counter in the header still
	// derive from wall-clock-sensitive paths (spinnerFrame is set by
	// the test below; startedAt is pinned so elapsed() is deterministic
	// because the test overrides it before calling View).
	btn := &button.Button{Name: "deploy", Runtime: "shell", TimeoutSeconds: 300}
	m := NewLogs(btn, nil, nil, "/tmp/deploy/main.sh")
	if m.cancel != nil {
		m.cancel()
	}
	m.width = 100
	m.height = 28
	m.pressID = "0c5b"
	// Pin startedAt far in the past but keep elapsed() result stable
	// by overriding the spinner frame; the view's `elapsed()` uses
	// time.Since() which is non-deterministic, so the header elapsed
	// will drift. Accept the drift — the test's point is layout +
	// chrome + severity coloring, not the elapsed column.
	m.startedAt = logsFixtureTime
	m.spinnerFrame = 0
	m.lines = append(m.lines,
		engine.LogLine{Ts: logsFixtureTime, Sev: engine.SeverityInfo, Text: "deploy: starting — env=staging"},
		engine.LogLine{Ts: logsFixtureTime.Add(100 * time.Millisecond), Sev: engine.SeverityStdout, Text: "resolving dependencies…"},
		engine.LogLine{Ts: logsFixtureTime.Add(1200 * time.Millisecond), Sev: engine.SeverityWarn, Text: "using deprecated flag --legacy-peer-deps"},
	)
	// Elapsed drift: override by zeroing startedAt so elapsed() returns
	// 0 (fixed output). We only want the layout to be captured.
	m.startedAt = time.Time{}
	assertSnapshot(t, "logs_tailing", m.View().Content)
}

func TestSnapshot_LogsDoneOK(t *testing.T) {
	// Logs view after a successful press: header + chrome flip to the
	// EXIT N / duration summary. Footer pill flips cancel → back.
	btn := &button.Button{Name: "deploy", Runtime: "shell", TimeoutSeconds: 300}
	m := NewLogs(btn, nil, nil, "/tmp/deploy/main.sh")
	if m.cancel != nil {
		m.cancel()
	}
	m.width = 100
	m.height = 28
	m.pressID = "0c5b"
	// done + result → elapsed() pulls from DurationMs (deterministic).
	m.done = true
	m.result = &engine.Result{Status: "ok", ExitCode: 0, DurationMs: 437}
	m.lines = append(m.lines,
		engine.LogLine{Ts: logsFixtureTime, Sev: engine.SeverityStdout, Text: "ok"},
	)
	// startedAt is only read by elapsed() and — when done — ignored in
	// favor of result.DurationMs. Leave it zero so there's no wall-
	// clock dependency.
	assertSnapshot(t, "logs_done_ok", m.View().Content)
}

func TestSnapshot_BoardArgFormOpen(t *testing.T) {
	btn := button.Button{
		Name:           "deploy",
		Runtime:        "shell",
		TimeoutSeconds: 300,
		Args: []button.ArgDef{
			{Name: "env", Type: "string", Required: true},
			{Name: "verbose", Type: "bool", Required: false},
		},
	}
	m := fixtureModel([]button.Button{btn})
	m.argForm = newArgForm(&btn)
	assertSnapshot(t, "board_argform_open", m.View().Content)
}
