package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/engine"
)

// keyPress constructs a synthetic KeyPressMsg whose String() matches
// the provided text — what handleKey's switch compares against.
func keyPress(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Text: s, Code: []rune(s)[0]})
}

// newTestLogsModel builds a LogsModel suitable for Update tests —
// skips the press goroutine entirely so tests can drive the model
// with synthetic messages.
func newTestLogsModel() LogsModel {
	btn := &button.Button{
		Name:           "test-btn",
		Runtime:        "shell",
		TimeoutSeconds: 5,
	}
	m := NewLogs(btn, nil, nil, "")
	// Cancel the startup context so the nonexistent press (empty
	// codePath) doesn't leak. Tests don't drive Init.
	if m.cancel != nil {
		m.cancel()
	}
	return *m
}

func TestLogsUpdate_LogLineAppends(t *testing.T) {
	m := newTestLogsModel()
	if len(m.lines) != 0 {
		t.Fatalf("starting lines = %d, want 0", len(m.lines))
	}
	msg := logLineMsg{line: engine.LogLine{
		Ts:   time.Now(),
		Sev:  engine.SeverityStdout,
		Text: "hello",
	}}
	next, cmd := m.Update(msg)
	nm := next.(LogsModel)
	if len(nm.lines) != 1 || nm.lines[0].Text != "hello" {
		t.Errorf("lines after log = %+v", nm.lines)
	}
	if cmd == nil {
		t.Error("expected waitForLine re-arm cmd, got nil")
	}
}

func TestLogsUpdate_ChannelClosedDoesNotReArm(t *testing.T) {
	m := newTestLogsModel()
	_, cmd := m.Update(logsChannelClosedMsg{})
	if cmd != nil {
		t.Errorf("channel-closed should not return a cmd, got %T", cmd)
	}
}

func TestLogsUpdate_DoneFlipsDone(t *testing.T) {
	m := newTestLogsModel()
	if m.done {
		t.Fatal("model starts done?")
	}
	msg := logsDoneMsg{result: &engine.Result{Status: "ok", ExitCode: 0, DurationMs: 150}}
	next, _ := m.Update(msg)
	nm := next.(LogsModel)
	if !nm.done {
		t.Error("done should be true after logsDoneMsg")
	}
	if nm.result == nil || nm.result.ExitCode != 0 {
		t.Errorf("result not stored: %+v", nm.result)
	}
}

func TestLogsUpdate_FollowToggle(t *testing.T) {
	m := newTestLogsModel()
	if !m.follow {
		t.Fatal("follow should start true")
	}
	next, _ := m.Update(keyPress("f"))
	nm := next.(LogsModel)
	if nm.follow {
		t.Error("follow should be false after 'f'")
	}
	// Toggle back
	next, _ = nm.Update(keyPress("f"))
	nm = next.(LogsModel)
	if !nm.follow {
		t.Error("follow should be true after second 'f'")
	}
}

func TestLogsUpdate_GJumpsToTop(t *testing.T) {
	m := newTestLogsModel()
	m.scrollTop = 99
	next, _ := m.Update(keyPress("g"))
	nm := next.(LogsModel)
	if nm.scrollTop != 0 {
		t.Errorf("scrollTop after 'g' = %d, want 0", nm.scrollTop)
	}
	if nm.follow {
		t.Error("'g' should disable follow")
	}
}

func TestLogsUpdate_CapitalGEnablesFollow(t *testing.T) {
	m := newTestLogsModel()
	m.follow = false
	next, _ := m.Update(keyPress("G"))
	nm := next.(LogsModel)
	if !nm.follow {
		t.Error("'G' should enable follow")
	}
}

func TestLogsView_VisibleRange(t *testing.T) {
	m := newTestLogsModel()
	// 10 lines, height 4, follow mode → last 4 visible
	for i := 0; i < 10; i++ {
		m.lines = append(m.lines, engine.LogLine{Text: "x"})
	}
	start, end := m.visibleRange(4)
	if start != 6 || end != 10 {
		t.Errorf("follow range = [%d, %d), want [6, 10)", start, end)
	}
	// Scroll-lock at 3, height 4
	m.follow = false
	m.scrollTop = 3
	start, end = m.visibleRange(4)
	if start != 3 || end != 7 {
		t.Errorf("locked range = [%d, %d), want [3, 7)", start, end)
	}
	// Height exceeds count
	start, end = m.visibleRange(100)
	if start != 0 || end != 10 {
		t.Errorf("full range = [%d, %d), want [0, 10)", start, end)
	}
}
