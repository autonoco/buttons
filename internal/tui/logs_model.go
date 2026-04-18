package tui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/engine"
)

// LogsModel is the Bubble Tea model for `buttons logs <name>`. Scope
// is exactly one press: the model starts the press in Init, tails
// every line to the screen, and lets the user scroll / cancel /
// dismiss. Once the press completes, the stream freezes and the
// header flips from "running" to "exit N / duration".
type LogsModel struct {
	styles Styles

	// The press in question
	btn        *button.Button
	args       map[string]string
	batteries  map[string]string
	codePath   string
	startedAt  time.Time
	pressID    string

	// Engine streaming
	sink    chan engine.LogLine
	lines   []engine.LogLine
	ctx     context.Context
	cancel  context.CancelFunc

	// Terminal state
	result *engine.Result
	done   bool

	// View state
	follow       bool // true = pin to the tail of the stream
	scrollTop    int  // first visible line index when not following
	spinnerFrame int
	ticking      bool

	width, height int
}

// NewLogs constructs a LogsModel for one press. Caller pre-resolves
// args / batteries / codePath so this layer doesn't have to know
// about the button service or config discovery — same shape as
// cmd/press which does the resolution and hands over.
func NewLogs(btn *button.Button, args, batteries map[string]string, codePath string) *LogsModel {
	ctx, cancel := logsTimeoutContext(btn)
	return &LogsModel{
		styles:    BuildStyles(),
		btn:       btn,
		args:      args,
		batteries: batteries,
		codePath:  codePath,
		startedAt: time.Now(),
		pressID:   shortPressID(time.Now()),
		sink:      make(chan engine.LogLine, logsSinkBuffer),
		ctx:       ctx,
		cancel:    cancel,
		follow:    true,
	}
}

// Init kicks off the press, starts waiting for the first streamed
// line, and starts the spinner tick. Three commands batched — Bubble
// Tea runs them concurrently.
func (m LogsModel) Init() tea.Cmd {
	m.ticking = true
	return tea.Batch(
		streamPress(m.ctx, m.btn, m.args, m.batteries, m.sink, m.codePath),
		waitForLine(m.sink),
		tickCmd(),
	)
}

// shortPressID produces a compact, user-visible identifier for this
// press (e.g. "0c5b"). Uses the nanosecond part of the start time —
// no need for cryptographic uniqueness; this is just a label on the
// header so the user can tell two recent runs apart.
func shortPressID(t time.Time) string {
	return fmt.Sprintf("%04x", t.UnixNano()&0xffff)
}

// elapsed returns how long the press has been running (or how long
// it ran for, once done). Safe before Init — returns zero.
func (m LogsModel) elapsed() time.Duration {
	if m.startedAt.IsZero() {
		return 0
	}
	if m.done && m.result != nil {
		return time.Duration(m.result.DurationMs) * time.Millisecond
	}
	return time.Since(m.startedAt)
}
