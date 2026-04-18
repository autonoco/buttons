// Package tui implements the `buttons board` dashboard.
//
// The TUI is built on Bubble Tea v2 + Lip Gloss v2, with styles driven
// by the Buttons identity spec (see styles.go). Orange is reserved
// strictly for active / running state — never decoration.
package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/autonoco/buttons/internal/button"
)

// Run launches the board TUI. If `initial` is non-empty and matches a
// loaded button's name, the cursor starts on that button.
//
// Blocks until the user quits (q / ctrl+c). Returns any startup or
// runtime error from Bubble Tea. Normal exit returns nil.
func Run(svc *button.Service, initial string) error {
	m, err := New(svc, initial)
	if err != nil {
		return err
	}

	// In Bubble Tea v2, alt-screen and mouse mode are set on the tea.View
	// returned from View() rather than passed as NewProgram options.
	p := tea.NewProgram(m)
	_, err = p.Run()
	return err
}

// RunLogs launches the full-screen logs view for one press. Caller
// pre-resolves args / batteries / codePath so this layer stays
// ignorant of button.Service / config discovery — same handoff shape
// as cmd/press, so the CLI and board-integration paths both work.
//
// Blocks until the user dismisses the view (esc / q) or the process
// is killed. Returns any Bubble Tea runtime error. A cancelled press
// (ctrl+c) exits cleanly with nil, because canceling is a user action
// not a program error.
func RunLogs(btn *button.Button, args, batteries map[string]string, codePath string) error {
	m := NewLogs(btn, args, batteries, codePath)
	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}

// RunDetail launches the structured detail page for one button and
// returns the exited model. Callers can inspect the returned model's
// EditRequested() to decide whether to shell out to $EDITOR on the
// code path — pulling the exec out of the TUI keeps the model pure.
// Returns (nil, err) on Bubble Tea runtime errors.
func RunDetail(m *DetailModel) (*DetailModel, error) {
	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		return nil, err
	}
	exited, _ := final.(DetailModel)
	return &exited, nil
}
