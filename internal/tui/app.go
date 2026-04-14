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
