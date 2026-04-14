package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/engine"
)

// runStatus tracks what we last observed for each button. It's only
// meaningful for buttons we've actually pressed this session — on
// startup everything is statusIdle.
type runStatus int

const (
	statusIdle runStatus = iota
	statusRunning
	statusOK
	statusFailed
)

// Model is the Bubble Tea model for `buttons board`. It holds the list
// of loaded buttons, the cursor position, and per-button run status.
type Model struct {
	svc    *button.Service
	styles Styles

	// All buttons, loaded once on startup. Pinned entries render at the
	// top as big cards; every button (pinned or not) also appears in the
	// list below.
	buttons []button.Button

	// Cursor selects either a pinned card (cursorPinned) or a list row
	// (cursorList). Kept as two separate indices so switching focus
	// between the two sections doesn't lose where you were in either.
	section      section
	cursorPinned int
	cursorList   int

	// Per-button status keyed by button name. A button not in the map
	// has never been pressed this session (statusIdle).
	status map[string]runStatus

	// Press error for the most recent press, if any. Cleared on next press.
	lastErr string

	// Terminal dims for responsive layout.
	width, height int
}

type section int

const (
	sectionPinned section = iota
	sectionList
)

// pressDoneMsg is dispatched when a background press finishes. The
// result may be nil on fatal errors (e.g. button disappeared).
type pressDoneMsg struct {
	name   string
	result *engine.Result
	err    error
}

// ------------------------------------------------------------------
// Model lifecycle
// ------------------------------------------------------------------

// New constructs a board model. If `initial` is non-empty and matches
// a loaded button, the cursor starts there.
func New(svc *button.Service, initial string) (*Model, error) {
	buttons, err := svc.List()
	if err != nil {
		return nil, fmt.Errorf("failed to load buttons: %w", err)
	}

	m := &Model{
		svc:     svc,
		styles:  BuildStyles(),
		buttons: buttons,
		status:  map[string]runStatus{},
		section: sectionList,
	}
	if m.hasPinned() {
		m.section = sectionPinned
	}

	if initial != "" {
		m.focusByName(initial)
	}

	return m, nil
}

func (m Model) Init() tea.Cmd {
	return nil
}

// ------------------------------------------------------------------
// Update
// ------------------------------------------------------------------

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.MouseClickMsg:
		if msg.Mouse().Button == tea.MouseLeft {
			return m.handleLeftClick(msg.Mouse().X, msg.Mouse().Y)
		}
		return m, nil

	case pressDoneMsg:
		return m.handlePressDone(msg), nil
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "up", "k":
		return m.moveCursor(-1), nil

	case "down", "j":
		return m.moveCursor(1), nil

	case "left", "h":
		// Left/right jumps across pinned cards when in pinned section,
		// otherwise switches focus from list back up to pinned.
		if m.section == sectionPinned {
			return m.movePinnedCursor(-1), nil
		}
		if m.hasPinned() {
			m.section = sectionPinned
			return m, nil
		}
		return m, nil

	case "right", "l":
		if m.section == sectionPinned {
			return m.movePinnedCursor(1), nil
		}
		return m, nil

	case "tab":
		// Toggle focus between pinned row and list.
		if m.section == sectionList && m.hasPinned() {
			m.section = sectionPinned
		} else {
			m.section = sectionList
		}
		return m, nil

	case "enter", " ":
		name := m.currentButtonName()
		if name == "" {
			return m, nil
		}
		return m.pressButton(name)
	}

	return m, nil
}

func (m Model) moveCursor(delta int) tea.Model {
	switch m.section {
	case sectionPinned:
		return m.movePinnedCursor(delta)
	case sectionList:
		return m.moveListCursor(delta)
	}
	return m
}

func (m Model) movePinnedCursor(delta int) tea.Model {
	pinned := m.pinned()
	if len(pinned) == 0 {
		return m
	}
	m.cursorPinned = (m.cursorPinned + delta + len(pinned)) % len(pinned)
	return m
}

func (m Model) moveListCursor(delta int) tea.Model {
	if len(m.buttons) == 0 {
		return m
	}
	m.cursorList = (m.cursorList + delta + len(m.buttons)) % len(m.buttons)
	return m
}

// handleLeftClick figures out whether the click landed on a pinned card
// or a list row by Y coordinate, then selects + presses it in one shot
// (matches the mockup's "click to press" interaction model).
func (m Model) handleLeftClick(x, y int) (tea.Model, tea.Cmd) {
	// Click detection is best-effort based on the known row offsets from
	// View(). Keeping it approximate rather than pixel-perfect — if the
	// layout drifts, clicks just no-op instead of silently mispressing.
	pinned := m.pinned()

	// Pinned row sits at roughly y=4..8 depending on border padding.
	// List starts after the pinned row + divider.
	pinnedBottom := 0
	if len(pinned) > 0 {
		pinnedBottom = pinnedRowTop + pinnedRowHeight
		if y >= pinnedRowTop && y < pinnedBottom {
			// Pick the pinned card under x.
			idx := pinnedIndexAtX(pinned, x)
			if idx >= 0 {
				m.section = sectionPinned
				m.cursorPinned = idx
				return m.pressButton(pinned[idx].Name)
			}
		}
	}

	// List rows each occupy one line starting at listTop.
	listTop := pinnedBottom + dividerHeight
	if len(pinned) == 0 {
		listTop = pinnedRowTop
	}
	rowIdx := y - listTop
	if rowIdx >= 0 && rowIdx < len(m.buttons) {
		m.section = sectionList
		m.cursorList = rowIdx
		return m.pressButton(m.buttons[rowIdx].Name)
	}

	return m, nil
}

// pressButton kicks off a press for the named button. It marks the
// button as running immediately so the UI flips to active state, then
// returns a tea.Cmd that runs the press in the background and reports
// back with a pressDoneMsg.
func (m Model) pressButton(name string) (tea.Model, tea.Cmd) {
	// Block re-press while another press is in flight — keeps the UI
	// honest about what "ACTIVE" means. A future smash-style concurrent
	// mode would relax this.
	for _, s := range m.status {
		if s == statusRunning {
			return m, nil
		}
	}

	var btn *button.Button
	for i := range m.buttons {
		if m.buttons[i].Name == name {
			btn = &m.buttons[i]
			break
		}
	}
	if btn == nil {
		return m, nil
	}

	// Skip buttons with required args — the TUI doesn't have a form yet.
	// Print a readable message; the user can press from the CLI with --arg.
	for _, a := range btn.Args {
		if a.Required {
			m.lastErr = fmt.Sprintf("%s requires --arg %s; press from the CLI for now", name, a.Name)
			return m, nil
		}
	}

	m.lastErr = ""
	m.status[name] = statusRunning

	codePath, _ := m.svc.CodePath(name)
	return m, runPress(btn, codePath)
}

// runPress is the tea.Cmd that actually executes the button via the
// engine. Kept as a package-level helper so the closure over btn+path
// is narrow and obvious.
func runPress(btn *button.Button, codePath string) tea.Cmd {
	name := btn.Name
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(btn.TimeoutSeconds)*time.Second)
		defer cancel()

		result := engine.Execute(ctx, btn, nil, codePath)
		return pressDoneMsg{name: name, result: result}
	}
}

func (m Model) handlePressDone(msg pressDoneMsg) Model {
	if msg.err != nil || msg.result == nil || msg.result.Status != "ok" {
		m.status[msg.name] = statusFailed
		if msg.err != nil {
			m.lastErr = fmt.Sprintf("press %s: %v", msg.name, msg.err)
		} else if msg.result != nil {
			errType := msg.result.ErrorType
			if errType == "" {
				errType = msg.result.Status
			}
			m.lastErr = fmt.Sprintf("press %s: %s", msg.name, errType)
		}
		return m
	}
	m.status[msg.name] = statusOK
	return m
}

// ------------------------------------------------------------------
// View
// ------------------------------------------------------------------

// Layout offset constants — kept in one place so click detection and
// rendering stay in sync.
const (
	headerHeight    = 2 // "buttons ... btn—XX | board"
	dividerHeight   = 1
	pinnedRowTop    = headerHeight + dividerHeight + 1 // blank line between divider and pinned
	pinnedRowHeight = 5                                // 1 top border + 1 pad + 1 text + 1 pad + 1 bottom border
)

func (m Model) View() tea.View {
	var content string
	if len(m.buttons) == 0 {
		content = m.emptyView()
	} else {
		var b strings.Builder
		b.WriteString(m.renderHeader())
		b.WriteString("\n")
		b.WriteString(m.renderDivider())
		b.WriteString("\n")

		if m.hasPinned() {
			b.WriteString("\n")
			b.WriteString(m.renderPinned())
			b.WriteString("\n\n")
			b.WriteString(m.renderDivider())
			b.WriteString("\n")
		}

		b.WriteString("\n")
		b.WriteString(m.renderList())
		b.WriteString("\n\n")
		b.WriteString(m.renderDivider())
		b.WriteString("\n\n")
		b.WriteString(m.renderFooter())
		content = b.String()
	}

	v := tea.NewView(content)
	// AltScreen clears the terminal on entry and restores it on exit so
	// the TUI doesn't leave rendering artifacts over the shell prompt.
	v.AltScreen = true
	// Cell-motion mouse reports clicks + button-held drags; enough for
	// click-to-press without the overhead of tracking every stray motion.
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m Model) renderHeader() string {
	left := m.styles.Wordmark.Render("buttons")
	right := m.styles.Label.Render(fmt.Sprintf("btn—%02d", len(m.buttons))) +
		m.styles.Muted.Render(" | ") +
		m.styles.Label.Render("board")

	// Simple left-right split. Width-aware padding without Lip Gloss
	// Place() because Place() can clip on narrow terminals.
	gap := m.width - visibleLen(left) - visibleLen(right)
	if gap < 2 {
		gap = 2
	}
	return "  " + left + strings.Repeat(" ", gap-2) + right
}

func (m Model) renderDivider() string {
	width := m.width
	if width <= 4 {
		width = 60
	}
	return m.styles.Divider.Render(strings.Repeat("─", width))
}

func (m Model) renderPinned() string {
	pinned := m.pinned()
	if len(pinned) == 0 {
		return ""
	}

	cards := make([]string, len(pinned))
	for i, btn := range pinned {
		cards[i] = m.renderPinnedCard(btn, i == m.cursorPinned && m.section == sectionPinned)
	}

	return "  " + lipgloss.JoinHorizontal(lipgloss.Top, cards...)
}

func (m Model) renderPinnedCard(btn button.Button, selected bool) string {
	style := m.styles.PinnedIdle
	if m.status[btn.Name] == statusRunning {
		style = m.styles.PinnedActive
	} else if selected {
		style = m.styles.PinnedSelected
	}
	// Pad card name so very short names still give a nice-looking card.
	label := btn.Name
	if len(label) < 10 {
		label = fmt.Sprintf("%-10s", label)
	}
	return style.Render(label) + "  "
}

func (m Model) renderList() string {
	if len(m.buttons) == 0 {
		return ""
	}

	lines := make([]string, len(m.buttons))
	for i, btn := range m.buttons {
		lines[i] = m.renderListRow(btn, i)
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderListRow(btn button.Button, idx int) string {
	active := m.status[btn.Name] == statusRunning
	selected := m.section == sectionList && idx == m.cursorList

	glyph := m.styles.indicator(active)

	var name string
	switch {
	case active:
		name = m.styles.ButtonNameActive.Render(btn.Name)
	case selected:
		name = m.styles.ButtonNameSelected.Render("> " + btn.Name)
	default:
		name = m.styles.ButtonName.Render("  " + btn.Name)
	}

	meta := m.styles.Secondary.Render(metaFor(btn))

	row := fmt.Sprintf("  %s %-32s %s", glyph, name, meta)

	if active {
		row += "  " + m.styles.BadgeActive.Render("ACTIVE")
	}
	return row
}

func (m Model) renderFooter() string {
	// Primary "press" action uses the filled-black style from the
	// workflow_engine mockup; secondary "quit" is outlined.
	quit := m.styles.ActionSecondary.Render("quit")

	var press string
	if m.section == sectionPinned || m.section == sectionList {
		press = m.styles.ActionPrimary.Render("press")
	} else {
		press = m.styles.ActionPrimaryDisabled.Render("press")
	}

	hints := m.styles.Muted.Render("↑↓ nav · tab switch · enter press · q quit")

	// Aligned left for actions, right for hints.
	left := "  " + quit + "  " + press
	gap := m.width - visibleLen(left) - visibleLen(hints)
	if gap < 2 {
		gap = 2
	}
	footer := left + strings.Repeat(" ", gap-2) + hints + "  "

	if m.lastErr != "" {
		footer += "\n\n  " + m.styles.Indicator.Render("! "+m.lastErr)
	}

	return footer
}

func (m Model) emptyView() string {
	return "\n  " + m.styles.Wordmark.Render("buttons") + "\n\n" +
		"  " + m.styles.Secondary.Render("no buttons yet. run `buttons create <name>` to get started.") + "\n\n" +
		"  " + m.styles.ActionSecondary.Render("quit") + "\n"
}

// ------------------------------------------------------------------
// Helpers
// ------------------------------------------------------------------

func (m Model) hasPinned() bool {
	for _, b := range m.buttons {
		if b.Pinned {
			return true
		}
	}
	return false
}

func (m Model) pinned() []button.Button {
	out := make([]button.Button, 0, len(m.buttons))
	for _, b := range m.buttons {
		if b.Pinned {
			out = append(out, b)
		}
	}
	return out
}

func (m Model) currentButtonName() string {
	switch m.section {
	case sectionPinned:
		pinned := m.pinned()
		if len(pinned) == 0 {
			return ""
		}
		return pinned[m.cursorPinned%len(pinned)].Name
	case sectionList:
		if len(m.buttons) == 0 {
			return ""
		}
		return m.buttons[m.cursorList%len(m.buttons)].Name
	}
	return ""
}

func (m *Model) focusByName(name string) {
	for i, b := range m.buttons {
		if b.Name == name {
			m.section = sectionList
			m.cursorList = i
			return
		}
	}
}

// metaFor returns the right-hand metadata string shown next to each
// button in the list view, condensed to one line: runtime + timeout +
// the most informative extra (URL for http, arg count otherwise).
func metaFor(b button.Button) string {
	parts := []string{b.Runtime, fmt.Sprintf("%ds", b.TimeoutSeconds)}
	switch b.Runtime {
	case "http":
		if b.URL != "" {
			method := b.Method
			if method == "" {
				method = "GET"
			}
			parts = append(parts, method+" "+shortenURL(b.URL))
		}
	default:
		if n := len(b.Args); n > 0 {
			parts = append(parts, fmt.Sprintf("%d arg", n))
		}
	}
	return strings.Join(parts, " · ")
}

func shortenURL(u string) string {
	// Strip protocol for display only. Never used for any network I/O.
	for _, prefix := range []string{"https://", "http://"} {
		if strings.HasPrefix(u, prefix) {
			return u[len(prefix):]
		}
	}
	return u
}

// visibleLen returns the number of terminal cells a string occupies
// after stripping ANSI escapes. Lip Gloss renders styled strings with
// escape codes that inflate len() but not display width.
func visibleLen(s string) int {
	return lipgloss.Width(s)
}

// pinnedIndexAtX returns the index of the pinned card whose horizontal
// span contains x, or -1. Cards start at column 2 (the leading "  "
// indent) and are separated by two-space gutters.
func pinnedIndexAtX(pinned []button.Button, x int) int {
	col := 2
	for i, b := range pinned {
		label := b.Name
		if len(label) < 10 {
			label = fmt.Sprintf("%-10s", label)
		}
		// card width = len(label) + padding 3 each side + 2 borders
		cardWidth := len(label) + 8
		if x >= col && x < col+cardWidth {
			return i
		}
		col += cardWidth + 2 // 2-space gutter between cards
	}
	return -1
}
