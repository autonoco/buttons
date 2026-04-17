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
// of loaded buttons, the cursor position, per-button run status, and a
// spinner frame used while any press is in flight.
type Model struct {
	svc    *button.Service
	styles Styles

	buttons []button.Button

	section      section
	cursorPinned int
	cursorList   int

	status map[string]runStatus

	lastErr string
	lastOK  string // transient success toast (name of last button that returned ok)

	spinnerFrame int
	ticking      bool

	width, height int
}

type section int

const (
	sectionPinned section = iota
	sectionList
)

type pressDoneMsg struct {
	name   string
	result *engine.Result
	err    error
}

// tickMsg drives the spinner. We only schedule ticks while at least one
// button is in statusRunning, so idle boards don't spin needlessly.
type tickMsg time.Time

const tickInterval = 90 * time.Millisecond

// ------------------------------------------------------------------
// Model lifecycle
// ------------------------------------------------------------------

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

func (m Model) Init() tea.Cmd { return nil }

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

	case tickMsg:
		m.spinnerFrame++
		if m.anyRunning() {
			return m, tickCmd()
		}
		m.ticking = false
		return m, nil
	}

	return m, nil
}

func tickCmd() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
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

// handleLeftClick routes a terminal click to the right action using the
// layout map computed the same way View() composes the screen. Click
// targets (in order of priority): pinned cards, list rows, footer quit,
// footer press.
func (m Model) handleLeftClick(x, y int) (tea.Model, tea.Cmd) {
	l := m.computeLayout()

	// Footer action hitboxes — take priority over content so clicks at
	// the bottom of the screen never accidentally press a list row.
	if y >= l.footerY0 && y <= l.footerY1 {
		if x >= l.quitX0 && x < l.quitX1 {
			return m, tea.Quit
		}
		if l.pressEnabled && x >= l.pressX0 && x < l.pressX1 {
			name := m.currentButtonName()
			if name == "" {
				return m, nil
			}
			return m.pressButton(name)
		}
		return m, nil
	}

	// Pinned row.
	pinned := m.pinned()
	if len(pinned) > 0 && y >= l.pinnedY0 && y <= l.pinnedY1 {
		idx := pinnedIndexAtX(pinned, x)
		if idx >= 0 {
			m.section = sectionPinned
			m.cursorPinned = idx
			return m.pressButton(pinned[idx].Name)
		}
		return m, nil
	}

	// List row.
	if len(m.buttons) > 0 && y >= l.listY0 && y <= l.listY1 {
		rowIdx := y - l.listY0
		if rowIdx >= 0 && rowIdx < len(m.buttons) {
			m.section = sectionList
			m.cursorList = rowIdx
			return m.pressButton(m.buttons[rowIdx].Name)
		}
	}

	return m, nil
}

// pressButton kicks off a press for the named button. Blocks concurrent
// presses (one running at a time), skips buttons with required args
// (TUI has no arg form yet — press those from the CLI), and starts the
// spinner tick if it isn't already running.
func (m Model) pressButton(name string) (tea.Model, tea.Cmd) {
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

	for _, a := range btn.Args {
		if a.Required {
			m.lastErr = fmt.Sprintf("%s requires --arg %s; press from the CLI for now", name, a.Name)
			m.lastOK = ""
			return m, nil
		}
	}

	m.lastErr = ""
	m.lastOK = ""
	m.status[name] = statusRunning

	codePath, _ := m.svc.CodePath(name)
	pressCmd := runPress(btn, codePath)

	// Start the spinner only if not already ticking — avoids tick storm.
	if !m.ticking {
		m.ticking = true
		return m, tea.Batch(pressCmd, tickCmd())
	}
	return m, pressCmd
}

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
		m.lastOK = ""
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
	m.lastErr = ""
	m.lastOK = msg.name
	return m
}

// ------------------------------------------------------------------
// Layout
// ------------------------------------------------------------------

// layout records the Y / X ranges of every interactive region. It is
// recomputed each View() and each click event from the same model
// state, so the two stay in lockstep without stashing mutable layout
// state on the model (which Bubble Tea's value-receiver pattern fights).
type layout struct {
	pinnedY0, pinnedY1 int
	listY0, listY1     int
	footerY0, footerY1 int
	quitX0, quitX1     int
	pressX0, pressX1   int
	pressEnabled       bool
}

const (
	leftPad       = 2
	pinnedHeight  = 5 // top border + pad + text + pad + bottom border
	footerHeight  = 3 // bordered action pill height
	actionGap     = 2 // spaces between quit and press pills
	headerHeight  = 1
	dividerHeight = 1
	sectionBlank  = 1
)

func (m Model) computeLayout() layout {
	l := layout{}

	// Y = 0 header ; Y = 1 divider ; Y = 2 blank ; Y = 3+ content begins.
	y := headerHeight + dividerHeight + sectionBlank

	if len(m.buttons) == 0 {
		// Empty hero: variable height, but we only need footer Y for clicks.
		hero := m.renderEmptyHero()
		y += countLines(hero) + sectionBlank + dividerHeight + sectionBlank
	} else {
		if m.hasPinned() {
			l.pinnedY0 = y
			l.pinnedY1 = y + pinnedHeight - 1
			y = l.pinnedY1 + 1 + sectionBlank + dividerHeight + sectionBlank
		}
		l.listY0 = y
		l.listY1 = y + len(m.buttons) - 1
		y = l.listY1 + 1 + sectionBlank + dividerHeight + sectionBlank
	}

	l.footerY0 = y
	l.footerY1 = y + footerHeight - 1

	// Footer action X ranges. Action pills are bordered boxes:
	// width = padding(2) + label + padding(2) + border(2) = label + 6.
	quitW := len("quit") + 6
	pressW := len("press") + 6
	l.quitX0 = leftPad
	l.quitX1 = l.quitX0 + quitW
	l.pressX0 = l.quitX1 + actionGap
	l.pressX1 = l.pressX0 + pressW

	l.pressEnabled = m.currentButtonName() != ""

	return l
}

// ------------------------------------------------------------------
// View
// ------------------------------------------------------------------

func (m Model) View() tea.View {
	var parts []string

	parts = append(parts, m.renderHeader())
	parts = append(parts, m.renderDivider())
	parts = append(parts, "") // blank before content

	if len(m.buttons) == 0 {
		parts = append(parts, m.renderEmptyHero())
	} else {
		if m.hasPinned() {
			parts = append(parts, m.renderPinned())
			parts = append(parts, "")
			parts = append(parts, m.renderDivider())
			parts = append(parts, "")
		}
		parts = append(parts, m.renderList())
	}

	parts = append(parts, "")
	parts = append(parts, m.renderDivider())
	parts = append(parts, "")
	parts = append(parts, m.renderFooter())

	if status := m.renderStatus(); status != "" {
		parts = append(parts, "")
		parts = append(parts, status)
	}

	content := strings.Join(parts, "\n")

	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m Model) renderHeader() string {
	left := m.styles.Wordmark.Render("buttons")
	right := m.styles.Label.Render(fmt.Sprintf("btn—%02d", len(m.buttons))) +
		m.styles.Muted.Render(" · ") +
		m.styles.Label.Render("board")

	w := m.width
	if w <= 0 {
		w = 80
	}
	gap := w - visibleLen(left) - visibleLen(right) - leftPad*2
	if gap < 2 {
		gap = 2
	}
	return strings.Repeat(" ", leftPad) + left + strings.Repeat(" ", gap) + right
}

func (m Model) renderDivider() string {
	w := m.width
	if w <= 4 {
		w = 80
	}
	return m.styles.Divider.Render(strings.Repeat("─", w))
}

func (m Model) renderPinned() string {
	pinned := m.pinned()
	if len(pinned) == 0 {
		return ""
	}

	cards := make([]string, 0, len(pinned)*2)
	for i, btn := range pinned {
		if i > 0 {
			cards = append(cards, "  ")
		}
		cards = append(cards, m.renderPinnedCard(btn, i == m.cursorPinned && m.section == sectionPinned))
	}

	row := lipgloss.JoinHorizontal(lipgloss.Top, cards...)
	return indentBlock(row, leftPad)
}

func (m Model) renderPinnedCard(btn button.Button, selected bool) string {
	style := m.styles.PinnedIdle
	if m.status[btn.Name] == statusRunning {
		style = m.styles.PinnedActive
	} else if selected {
		style = m.styles.PinnedSelected
	}
	label := btn.Name
	if len(label) < 10 {
		label = fmt.Sprintf("%-10s", label)
	}
	return style.Render(label)
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
	done := m.status[btn.Name] == statusOK
	failed := m.status[btn.Name] == statusFailed
	selected := m.section == sectionList && idx == m.cursorList

	// Spinner frame only while running; success/failure get static glyphs.
	var glyph string
	switch {
	case active:
		glyph = m.styles.indicator(true, m.spinnerFrame)
	case done:
		glyph = m.styles.StatusOK.Render("✓")
	case failed:
		glyph = m.styles.StatusError.Render("✗")
	default:
		glyph = m.styles.indicator(false, -1)
	}

	var name string
	switch {
	case active:
		name = m.styles.ButtonNameActive.Render(btn.Name)
	case selected:
		name = m.styles.ButtonNameSelected.Render("› " + btn.Name)
	default:
		name = m.styles.ButtonName.Render("  " + btn.Name)
	}

	meta := m.styles.Secondary.Render(metaFor(btn))

	row := fmt.Sprintf("%s%s %-32s %s", strings.Repeat(" ", leftPad), glyph, name, meta)

	if active {
		row += "  " + m.styles.BadgeActive.Render("ACTIVE")
	}
	return row
}

// renderEmptyHero is the empty-state content block. Designed to teach
// the user two real commands — a shell button and an HTTP button —
// instead of a generic "create something" placeholder.
func (m Model) renderEmptyHero() string {
	title := m.styles.HeroTitle.Render("no buttons yet")
	sub := m.styles.HeroBody.Render("buttons are reusable actions with typed args and structured output.")

	line := func(label, cmd string) string {
		return m.styles.HeroBody.Render(label) + "  " + m.styles.HeroCode.Render(cmd)
	}

	lines := []string{
		title,
		"",
		sub,
		"",
		line("a shell one-liner:", `buttons create hello --code 'echo hi'`),
		line("a URL:           ", `buttons create weather --url wttr.in/NYC`),
		"",
		m.styles.Muted.Render("press q to quit, or open a new terminal and run the commands above."),
	}

	hero := strings.Join(lines, "\n")
	return indentBlock(hero, leftPad*2)
}

// renderFooter composes the bordered action pills and the keybind hint
// row. Uses lipgloss.JoinHorizontal so multi-line bordered boxes
// align, then indentBlock to shift every rendered line by leftPad.
func (m Model) renderFooter() string {
	quit := m.styles.ActionSecondary.Render("quit")

	pressStyle := m.styles.ActionPrimary
	if !m.pressIsEnabled() {
		pressStyle = m.styles.ActionPrimaryDisabled
	}
	press := pressStyle.Render("press")

	actions := lipgloss.JoinHorizontal(
		lipgloss.Top,
		quit,
		strings.Repeat(" ", actionGap),
		press,
	)

	// Compose the hint line separately and tuck it into the middle row
	// of the action block using Place — visually the hints sit on the
	// same baseline as the pill labels.
	hints := m.composeHints()

	w := m.width
	if w <= 0 {
		w = 80
	}
	actionsW := lipgloss.Width(actions)
	hintsW := lipgloss.Width(hints)
	gap := w - actionsW - hintsW - leftPad*2
	if gap < 2 {
		gap = 2
	}

	hintBlock := lipgloss.Place(hintsW, lipgloss.Height(actions), lipgloss.Left, lipgloss.Center, hints)
	row := lipgloss.JoinHorizontal(lipgloss.Top, actions, strings.Repeat(" ", gap), hintBlock)

	return indentBlock(row, leftPad)
}

// composeHints renders a compact keybind legend. Each key gets a subtle
// chip so the affordance is visible without the noise of a full help box.
func (m Model) composeHints() string {
	pair := func(key, label string) string {
		return m.styles.KeyChip.Render(key) + m.styles.Muted.Render(" "+label)
	}
	sep := m.styles.Muted.Render("  ·  ")
	return pair("↑↓", "nav") + sep + pair("↵", "press") + sep + pair("q", "quit")
}

// renderStatus returns the single-line status/toast below the footer,
// or "" if there's nothing to say. Errors read red-indicator, successes
// read quieter so they don't compete visually.
func (m Model) renderStatus() string {
	if m.lastErr != "" {
		return strings.Repeat(" ", leftPad) + m.styles.StatusError.Render("!  "+m.lastErr)
	}
	if m.lastOK != "" {
		return strings.Repeat(" ", leftPad) + m.styles.StatusOK.Render("✓  pressed "+m.lastOK)
	}
	return ""
}

// pressIsEnabled returns true when there's a button to press under the
// current cursor — drives the primary action pill's visual state.
func (m Model) pressIsEnabled() bool {
	return m.currentButtonName() != ""
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

func (m Model) anyRunning() bool {
	for _, s := range m.status {
		if s == statusRunning {
			return true
		}
	}
	return false
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
	for _, prefix := range []string{"https://", "http://"} {
		if strings.HasPrefix(u, prefix) {
			return u[len(prefix):]
		}
	}
	return u
}

// visibleLen returns the number of terminal cells a string occupies
// after ANSI stripping. Lip Gloss Width does exactly this.
func visibleLen(s string) int { return lipgloss.Width(s) }

// indentBlock shifts every line of s by `cols` spaces. Essential for
// multi-line blocks (bordered boxes, joined rows) where a single
// concat with a leading indent only shifts the first line.
func indentBlock(s string, cols int) string {
	if cols <= 0 || s == "" {
		return s
	}
	pad := strings.Repeat(" ", cols)
	return pad + strings.ReplaceAll(s, "\n", "\n"+pad)
}

// countLines returns the number of rendered lines in a string. An empty
// string is treated as a single (blank) line so layout math stays tidy.
func countLines(s string) int {
	if s == "" {
		return 1
	}
	return strings.Count(s, "\n") + 1
}

// pinnedIndexAtX returns the index of the pinned card whose horizontal
// span contains x, or -1. Cards start at column leftPad and are
// separated by two-space gutters.
func pinnedIndexAtX(pinned []button.Button, x int) int {
	col := leftPad
	for i, b := range pinned {
		label := b.Name
		if len(label) < 10 {
			label = fmt.Sprintf("%-10s", label)
		}
		// card width = padding(6 = 3 each side) + label + border(2)
		cardWidth := len(label) + 8
		if x >= col && x < col+cardWidth {
			return i
		}
		col += cardWidth + 2 // 2-space gutter between cards
	}
	return -1
}
