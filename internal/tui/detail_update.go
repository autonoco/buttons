package tui

import (
	tea "charm.land/bubbletea/v2"
)

// Update handles keyboard / window-size messages. The detail page is
// intentionally shallow — no streams or animations — so there's no
// Init command, no tickMsg case, no mouse routing.
func (m DetailModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// handleKey routes the four supported keys:
//
//	e         request an $EDITOR session on the code path — actual
//	          exec happens in RunDetail after Bubble Tea exits, so
//	          the TUI doesn't have to juggle tea.ExecProcess lifecycle
//	q / esc   dismiss
//	↵         same as dismiss for now (press integration comes later;
//	          the detail view shows the exact press command so the
//	          user can copy / retype without a new round-trip)
func (m DetailModel) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc", "enter", "ctrl+c":
		return m, tea.Quit

	case "e":
		// Edit is only meaningful for runtime-backed buttons with a
		// resolved code path; for URL/prompt buttons, no-op so the
		// key doesn't silently do the wrong thing.
		if m.codePath == "" {
			return m, nil
		}
		m.editRequested = true
		return m, tea.Quit
	}
	return m, nil
}
