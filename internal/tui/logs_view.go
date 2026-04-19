package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/autonoco/buttons/internal/engine"
)

// View renders the logs screen as three blocks stacked vertically:
//
//	header     button · press id · elapsed + spinner
//	stream     the streamed lines, viewport-clipped to the terminal
//	footer     cancel pill + key-hint chips
//
// Layout is line-oriented: header is 2 lines, footer is 3 lines
// (bordered action pill height), the rest is the stream viewport.
func (m LogsModel) View() tea.View {
	var parts []string

	parts = append(parts, m.renderLogsHeader())
	parts = append(parts, m.divider())
	parts = append(parts, "")

	// Compute stream height so the footer never gets pushed off a
	// short terminal. Reserve space for header (2) + dividers (2) +
	// blanks (3) + footer (3) + optional status line.
	streamHeight := m.height - 10
	if streamHeight < 3 {
		streamHeight = 3
	}
	parts = append(parts, m.renderLogsStream(streamHeight))

	parts = append(parts, "")
	parts = append(parts, m.divider())
	parts = append(parts, "")
	parts = append(parts, m.renderLogsFooter())

	// Bottom chrome strip — same pattern as board and detail. Badges on
	// the right reflect the view's current mode: FOLLOW on while
	// tailing a live press, EXIT N once the press is done, TAIL when
	// scrolled away from the bottom.
	parts = append(parts, "")
	parts = append(parts, m.renderLogsChrome())

	content := strings.Join(parts, "\n")

	v := tea.NewView(content)
	v.AltScreen = true
	// Cell-motion mouse mode isn't needed here (no clickable
	// targets yet) but enabling it matches the board; future mouse-
	// scroll support will plug in without a mode flip.
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// renderLogsHeader draws the identity row + elapsed counter. Left
// side names the button and press; right side shows a live elapsed
// counter with an `● follow` indicator while tailing, or the final
// "exit N / duration" once done.
func (m LogsModel) renderLogsHeader() string {
	// Left — button identity
	name := m.styles.Wordmark.Render(m.btn.Name)
	rt := m.styles.Muted.Render(" — " + m.btn.Runtime)
	press := m.styles.Muted.Render("  ·  press " + m.pressID)
	left := name + rt + press

	// Right — while tailing: `● follow` (orange dot + label) next to
	// elapsed counter + spinner. Spec station 09: the indicator is
	// the affordance that says "this view is following the tail."
	// Once done: colored exit-N / duration summary.
	var right string
	if m.done && m.result != nil {
		exitFmt := fmt.Sprintf("exit %d · %s", m.result.ExitCode, formatElapsed(m.elapsed()))
		if m.result.Status != "ok" {
			right = m.styles.StatusError.Render(exitFmt)
		} else {
			right = m.styles.StatusOK.Render(exitFmt)
		}
	} else {
		spinner := m.styles.indicator(true, m.spinnerFrame)
		elapsed := m.styles.Muted.Render(formatElapsed(m.elapsed()))
		if m.follow {
			followTag := m.styles.ChromeActiveBadge.Render("● follow")
			right = followTag + m.styles.Muted.Render("  ·  ") + spinner + " " + elapsed
		} else {
			right = spinner + " " + elapsed
		}
	}

	w := m.width
	if w <= 0 {
		w = 80
	}
	gap := w - lipgloss.Width(left) - lipgloss.Width(right) - leftPad*2
	if gap < 2 {
		gap = 2
	}
	return strings.Repeat(" ", leftPad) + left + strings.Repeat(" ", gap) + right
}

// renderLogsChrome paints the bottom status strip for the logs view.
// Runs in the same pattern as board + detail:
//
//	while tailing    ● FOLLOW · PRESS <id>              (orange badge)
//	scroll-locked    TAIL · PRESS <id>                  (muted chrome)
//	done             EXIT N · <duration>                (colored summary)
func (m LogsModel) renderLogsChrome() string {
	left := strings.Join([]string{"TTY 1", "UTF-8", "256-COLOR"}, m.styles.Muted.Render(" · "))
	left = m.styles.Chrome.Render(left)

	var right string
	switch {
	case m.done && m.result != nil:
		status := m.styles.StatusOK
		if m.result.Status != "ok" {
			status = m.styles.StatusError
		}
		right = status.Render(fmt.Sprintf("EXIT %d · %s", m.result.ExitCode, formatElapsed(m.elapsed())))
	case m.follow:
		right = m.styles.ChromeActiveBadge.Render("● FOLLOW") +
			m.styles.Muted.Render(" · ") +
			m.styles.Chrome.Render("PRESS "+m.pressID)
	default:
		right = m.styles.Chrome.Render("TAIL · PRESS " + m.pressID)
	}

	w := m.width
	if w <= 0 {
		w = 80
	}
	gap := w - lipgloss.Width(left) - lipgloss.Width(right) - leftPad*2
	if gap < 2 {
		gap = 2
	}
	return strings.Repeat(" ", leftPad) + left + strings.Repeat(" ", gap) + right
}

// renderLogsStream paints the viewport-clipped log lines. In follow
// mode, the tail of the stream is always visible; scroll-locked mode
// honors m.scrollTop. Each line is rendered on one terminal row —
// long lines are truncated rune-aware (no word wrap).
func (m LogsModel) renderLogsStream(height int) string {
	if len(m.lines) == 0 {
		// Pre-first-line state. Show a subtle placeholder instead of
		// dead space so the user knows the view is alive.
		hint := "waiting for output…"
		if m.done {
			hint = "press completed with no output."
		}
		return indentBlock(m.styles.Muted.Render(hint), leftPad)
	}

	// Select visible slice based on follow/scroll state.
	start, end := m.visibleRange(height)
	visible := m.lines[start:end]

	// Width budget for line text: total width minus the indent and
	// the fixed-width timestamp + severity columns. Minimum floor
	// so narrow terminals still show something useful.
	previewBudget := m.width - leftPad - 24 /* ts + sev */
	if previewBudget < 20 {
		previewBudget = 20
	}

	rendered := make([]string, 0, len(visible))
	for _, ln := range visible {
		rendered = append(rendered, m.renderLogsLine(ln, previewBudget))
	}
	return indentBlock(strings.Join(rendered, "\n"), leftPad)
}

// visibleRange returns the [start, end) slice indices for the
// currently-visible portion of m.lines given the viewport height.
// In follow mode, always the last `height` lines. In scroll-lock
// mode, starts at m.scrollTop.
func (m LogsModel) visibleRange(height int) (int, int) {
	n := len(m.lines)
	if height >= n {
		return 0, n
	}
	if m.follow {
		return n - height, n
	}
	start := m.scrollTop
	if start > n-height {
		start = n - height
	}
	if start < 0 {
		start = 0
	}
	return start, start + height
}

// renderLogsLine formats one streamed line: hh:mm:ss.mmm · sev · text.
// Severity colors the middle chunk; the text itself stays in the
// readable primary / secondary role so stderr flood doesn't drown
// the visual field in orange.
func (m LogsModel) renderLogsLine(l engine.LogLine, previewBudget int) string {
	ts := l.Ts.Local().Format("15:04:05.000")
	tsCol := m.styles.Muted.Render(ts)

	var sevCol string
	switch l.Sev {
	case engine.SeverityStderr:
		sevCol = m.styles.StatusError.Render("err ")
	case engine.SeverityWarn:
		sevCol = m.styles.StatusWarn.Render("warn")
	case engine.SeverityInfo:
		sevCol = m.styles.Muted.Render("info")
	default: // stdout
		sevCol = m.styles.Secondary.Render("out ")
	}

	text := truncateDisplay(l.Text, previewBudget)
	// Prefix stdout lines with a `▸` arrow glyph so stream content
	// reads as "the program's voice" vs. info / warn / err which are
	// the program's status. Spec station 09.
	prefix := ""
	if l.Sev == engine.SeverityStdout {
		prefix = m.styles.Muted.Render("▸ ")
	}

	// stderr and warn lines get their text subtly shaded toward
	// the severity color so they read differently from stdout
	// without making the whole row loud.
	var textCol string
	switch l.Sev {
	case engine.SeverityStderr:
		textCol = m.styles.ButtonNameActive.Render(text)
	case engine.SeverityWarn:
		textCol = m.styles.StatusWarn.Render(text)
	default:
		textCol = m.styles.ButtonName.Render(text)
	}

	return fmt.Sprintf("%s  %s  %s%s", tsCol, sevCol, prefix, textCol)
}

// renderLogsFooter composes the action pill + key hints. Mirrors the
// board's footer shape so the visual vocabulary stays consistent.
func (m LogsModel) renderLogsFooter() string {
	label := "cancel"
	pressStyle := m.styles.ActionPrimary
	if m.done {
		label = "back"
		pressStyle = m.styles.ActionSecondary
	}
	pill := pressStyle.Render(label)

	hints := m.composeLogsHints()

	w := m.width
	if w <= 0 {
		w = 80
	}
	pillW := lipgloss.Width(pill)
	hintsW := lipgloss.Width(hints)
	gap := w - pillW - hintsW - leftPad*2
	if gap < 2 {
		gap = 2
	}
	hintBlock := lipgloss.Place(hintsW, lipgloss.Height(pill), lipgloss.Left, lipgloss.Center, hints)
	row := lipgloss.JoinHorizontal(lipgloss.Top, pill, strings.Repeat(" ", gap), hintBlock)
	return indentBlock(row, leftPad)
}

// divider renders a full-width muted rule. Duplicated from board's
// renderDivider so LogsModel doesn't need a pointer to the board
// model — same output, different receiver.
func (m LogsModel) divider() string {
	w := m.width
	if w <= 4 {
		w = 80
	}
	return m.styles.Divider.Render(strings.Repeat("─", w))
}

// composeLogsHints is the keybind legend for the logs view. Mirrors
// the chip + label style used on the board so muscle memory carries.
func (m LogsModel) composeLogsHints() string {
	pair := func(key, label string) string {
		return m.styles.KeyChip.Render(key) + m.styles.Muted.Render(" "+label)
	}
	sep := m.styles.Muted.Render("  ·  ")
	followLabel := "follow"
	if m.follow {
		followLabel = "∞ tail"
	}
	return pair("f", followLabel) + sep + pair("g/G", "top/end") + sep + pair("esc", "back")
}
