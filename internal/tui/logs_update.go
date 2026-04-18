package tui

import (
	tea "charm.land/bubbletea/v2"
)

// Update for the logs view. Same Elm-style contract as board's Model:
// pure, value-receiver, returns a new model + cmd. Never mutate the
// receiver in place.
func (m LogsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case logLineMsg:
		// Append the line. If the user is in follow mode, scroll stays
		// pinned to the tail by View; if they've scrolled away, lines
		// just accumulate out of sight and they can press `G` to come
		// back.
		m.lines = append(m.lines, msg.line)
		// Re-arm so the next line in the sink gets picked up.
		return m, waitForLine(m.sink)

	case logsChannelClosedMsg:
		// Sink is closed — streamPress finished and close()'d it.
		// Don't re-arm waitForLine; logsDoneMsg will (has?) arrived
		// with the final Result.
		return m, nil

	case logsDoneMsg:
		m.result = msg.result
		m.done = true
		return m, nil

	case tickMsg:
		m.spinnerFrame++
		// Keep ticking while the press is in flight. Once done, stop —
		// matches the board's policy of not cooking CPU on an idle view.
		if !m.done {
			return m, tickCmd()
		}
		return m, nil
	}

	return m, nil
}

// handleKey routes keyboard input. Most keys are dismissable by the
// user's muscle memory — esc/q exit, ctrl+c cancels the press.
// f toggles follow, g/G jump to the top / bottom.
func (m LogsModel) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		// Leave the logs view. Don't cancel an in-flight press — the
		// user explicitly asked to stop watching, not to kill the
		// process. ctrl+c is the dedicated cancel key.
		return m, tea.Quit

	case "ctrl+c":
		// Cancel the press context — engine.Execute will observe the
		// context done and kill the child's process group, then
		// streamPress will return with the final Result and close the
		// sink. Two ctrl+c's (or esc after cancel) dismiss the view.
		if m.cancel != nil && !m.done {
			m.cancel()
			return m, nil
		}
		return m, tea.Quit

	case "f":
		// Manually toggle follow. When off, new lines accumulate but
		// the visible window doesn't scroll — useful for reading a
		// specific line without the stream shoving it off-screen.
		m.follow = !m.follow
		return m, nil

	case "g":
		// Jump to top — disables follow so the user can scroll around.
		m.scrollTop = 0
		m.follow = false
		return m, nil

	case "G":
		// Jump to end — re-enables follow.
		m.follow = true
		m.scrollTop = 0 // View recomputes effective offset when follow is true
		return m, nil
	}

	return m, nil
}
