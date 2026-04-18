package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/history"
)

// DetailModel is the Bubble Tea model for `buttons <name>` in TTY
// mode — a structured, scannable detail page for one button.
// Replaces the ad-hoc stderr printing in cmd/detail.go when stdout is
// a terminal; the stderr / --json paths still live in cmd/detail.go
// for pipelines and scripts.
type DetailModel struct {
	styles Styles

	btn      *button.Button
	lastRun  *history.Run // nil if never pressed
	agentMD  string       // AGENT.md contents if non-default
	codePath string       // resolved on-disk code path for `e` edit

	// Terminal size; tracked for the footer right-alignment.
	width, height int

	// Exit intent — set when the user pressed e and wants to shell out
	// after quit. The caller (RunDetail) inspects this after p.Run()
	// returns.
	editRequested bool
}

// NewDetail constructs a detail view for btn. lastRun and agentMD are
// resolved by the caller (cmd/detail) so the model stays ignorant of
// the button.Service / config packages — same handoff shape as
// NewLogs.
func NewDetail(btn *button.Button, lastRun *history.Run, agentMD, codePath string) *DetailModel {
	return &DetailModel{
		styles:   BuildStyles(),
		btn:      btn,
		lastRun:  lastRun,
		agentMD:  agentMD,
		codePath: codePath,
	}
}

// Init does nothing — the detail view is fully static. No streaming,
// no tick, no background command.
func (m DetailModel) Init() tea.Cmd { return nil }

// EditRequested is set to true when the user pressed `e`. RunDetail
// reads this after Bubble Tea exits and decides whether to exec
// $EDITOR on the code path — pulling the editor invocation out of the
// TUI keeps the model pure and avoids tea.ExecProcess gymnastics.
func (m DetailModel) EditRequested() bool { return m.editRequested }
