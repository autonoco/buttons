package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/autonoco/buttons/internal/battery"
	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/engine"
	"github.com/autonoco/buttons/internal/history"
)

// logsPaneLimit caps how many historical runs the logs pane shows for
// the focused button. Chosen small so the pane never dominates the
// board on short terminals; users can drop to the CLI for deeper dives.
const logsPaneLimit = 5

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

	// cursor is the selected index into the card grid. The grid is the
	// pinned-first ordering returned by cardOrder() — pinned buttons
	// float to the front, unpinned follow. up/down nav moves by one
	// grid row (cardsPerRow() cells); left/right moves by 1.
	cursor int

	status map[string]runStatus

	// pressStartedAt records when a press began, keyed by button name,
	// so the view can render a live elapsed counter on the active
	// card. Cleared in handlePressDone when the press completes (or
	// fails) — stale entries would otherwise show "active · 83s" on a
	// row that finished half a minute ago.
	pressStartedAt map[string]time.Time

	lastErr string
	lastOK  string // transient success toast (name of last button that returned ok)

	spinnerFrame int
	ticking      bool

	// logsOpen toggles the logs pane that sits between the list and the
	// footer. Pane shows recent run history for the focused button.
	logsOpen bool

	// logsPaneCursor is the selected run row inside the logs pane.
	// Meaningful only while logsOpen; reset to 0 whenever the focused
	// button changes or the pane closes.
	logsPaneCursor int

	// logsDetail, when non-nil, replaces the board's main content with
	// a full-screen view of a single historical run's stdout/stderr.
	// Opened by pressing ↵ on a pane row; dismissed with esc.
	logsDetail    *history.Run
	logsDetailBtn string
	// logsDetailScroll is the top line index for the detail view's
	// scrollable body. Clamped in movePaneCursor / handleKey.
	logsDetailScroll int

	// argForm, when non-nil, is the inline press-with-args prompt
	// replacing the board's content area. Opened automatically when
	// the user presses a button with required args; dismissed on
	// submit (press fires) or esc (no press).
	argForm *argForm

	// pressPulse is the name of the button currently showing the
	// keydown / fire frames of the mechanical-press animation. Empty
	// when no press is mid-choreography. Distinct from status because
	// the pulse can flash even when the underlying press completes
	// synchronously (HTTP button returning in <180ms).
	pressPulse     string
	pressPulseFire bool

	width, height int
}

type pressDoneMsg struct {
	name   string
	result *engine.Result
	err    error
}

// pressFireMsg fires 40ms after keydown to swap the keydown (thick
// border) frame for the fire (orange border + fill) frame — the
// moment the user feels "I committed the press." Name is carried
// through so a second press stacked on the first doesn't dangle a
// stale pulse on a row that was already released.
type pressFireMsg struct{ name string }

// pressReleaseMsg fires 180ms after keydown and ends the choreography.
// If the press is still running at this point, the row falls back to
// the persistent PinnedActive / statusRunning state — visually
// continuous because both use the same thick-orange border.
type pressReleaseMsg struct{ name string }

const (
	pressFireDelay    = 40 * time.Millisecond
	pressReleaseDelay = 180 * time.Millisecond
)

// tickMsg drives the spinner. We only schedule ticks while at least one
// button is in statusRunning, so idle boards don't spin needlessly.
type tickMsg time.Time

// refreshMsg fires at a low cadence to re-list buttons from disk so a
// board left open sees buttons created (or deleted) in another terminal
// without the user having to close and reopen the window.
type refreshMsg time.Time

const (
	tickInterval    = 90 * time.Millisecond
	refreshInterval = 2 * time.Second
)

// ------------------------------------------------------------------
// Model lifecycle
// ------------------------------------------------------------------

func New(svc *button.Service, initial string) (*Model, error) {
	buttons, err := svc.List()
	if err != nil {
		return nil, fmt.Errorf("failed to load buttons: %w", err)
	}

	m := &Model{
		svc:            svc,
		styles:         BuildStyles(),
		buttons:        buttons,
		status:         map[string]runStatus{},
		pressStartedAt: map[string]time.Time{},
	}
	if initial != "" {
		m.focusByName(initial)
	}
	return m, nil
}

func (m Model) Init() tea.Cmd { return refreshCmd() }

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

	case pressFireMsg:
		// Only flip fire on if the pulse is still targeting the same
		// button. A rapid double-press would otherwise paint fire on a
		// row that already released.
		if m.pressPulse == msg.name {
			m.pressPulseFire = true
		}
		return m, nil

	case pressReleaseMsg:
		// End the pulse. If the underlying press is still running, the
		// row visually stays "active" via statusRunning → PinnedActive;
		// both frames share the thick-orange border so the transition
		// reads as one continuous gesture, not a flicker.
		if m.pressPulse == msg.name {
			m.pressPulse = ""
			m.pressPulseFire = false
		}
		return m, nil

	case tickMsg:
		m.spinnerFrame++
		if m.anyRunning() {
			return m, tickCmd()
		}
		m.ticking = false
		return m, nil

	case refreshMsg:
		// Re-list from disk. Errors are swallowed: a transient read
		// error during a CRUD race shouldn't empty the board; the next
		// tick will try again. The running status map stays as-is so a
		// press in flight keeps its spinner.
		if buttons, err := m.svc.List(); err == nil {
			m.buttons = buttons
			if m.cursor >= len(m.buttons) && len(m.buttons) > 0 {
				m.cursor = len(m.buttons) - 1
			}
		}
		return m, refreshCmd()
	}

	return m, nil
}

func tickCmd() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func refreshCmd() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg { return refreshMsg(t) })
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// When the arg form is open, every non-emergency key is its
	// property. Ctrl+C keeps its board-level meaning (quit) so there's
	// always an escape hatch even from inside a modal state.
	if m.argForm != nil {
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m.handleArgFormKey(msg)
	}

	switch msg.String() {
	// Ctrl+C is kept as an unadvertised emergency escape so the user
	// is never truly stuck if something goes wrong — but the board
	// otherwise reads as an ambient dashboard (no visible "quit"
	// affordance, no q keybind, no hint). It's UI for a human; agents
	// invoke the CLI. "Quit" in the way a prompt has quit is the wrong
	// mental model for this view.
	case "ctrl+c":
		return m, tea.Quit

	case "esc":
		// Peel back one layer of modal state per press: detail → pane
		// → nothing. Matches the vim / less mental model where esc
		// always shrinks context.
		if m.logsDetail != nil {
			m.logsDetail = nil
			m.logsDetailBtn = ""
			m.logsDetailScroll = 0
			return m, nil
		}
		if m.logsOpen {
			m.logsOpen = false
			m.logsPaneCursor = 0
			return m, nil
		}
		return m, nil

	case "up", "k":
		if m.logsDetail != nil {
			if m.logsDetailScroll > 0 {
				m.logsDetailScroll--
			}
			return m, nil
		}
		if m.logsOpen {
			return m.movePaneCursor(-1), nil
		}
		return m.moveByRow(-1), nil

	case "down", "j":
		if m.logsDetail != nil {
			m.logsDetailScroll++
			return m, nil
		}
		if m.logsOpen {
			return m.movePaneCursor(1), nil
		}
		return m.moveByRow(1), nil

	case "left", "h":
		m.logsPaneCursor = 0
		return m.moveCursor(-1), nil

	case "right", "l":
		m.logsPaneCursor = 0
		return m.moveCursor(1), nil

	case "tab":
		m.logsPaneCursor = 0
		return m.moveCursor(1), nil

	case "enter", " ":
		// Pane-open mode: ↵ opens the detail view for the selected
		// run rather than firing a press. The press CTA belongs to
		// the card grid, not the history pane.
		if m.logsOpen && m.logsDetail == nil {
			return m.openLogDetail(), nil
		}
		name := m.currentButtonName()
		if name == "" {
			return m, nil
		}
		return m.pressButton(name)

	case "L":
		// Shift+L toggles the logs pane. Lower-case `l` is already bound
		// to "move cursor right" (vim convention), so logs takes the
		// shifted variant; the hint chip surfaces the capital.
		m.logsOpen = !m.logsOpen
		if !m.logsOpen {
			m.logsPaneCursor = 0
		}
		return m, nil
	}

	return m, nil
}

// movePaneCursor shifts the logs-pane row cursor with wrap. Clamped to
// the number of runs actually fetched for the focused button so we
// never point past the visible list.
func (m Model) movePaneCursor(delta int) tea.Model {
	target := m.currentButtonName()
	if target == "" {
		return m
	}
	runs, err := history.List(target, logsPaneLimit)
	if err != nil || len(runs) == 0 {
		return m
	}
	n := len(runs)
	m.logsPaneCursor = ((m.logsPaneCursor+delta)%n + n) % n
	return m
}

// openLogDetail fetches the focused button's recent runs, picks the
// one the pane cursor points at, and stashes it on the model as the
// active detail view. A fresh List call is cheap (5 JSON reads) and
// guarantees the detail reflects runs completed since the pane first
// opened.
func (m Model) openLogDetail() tea.Model {
	target := m.currentButtonName()
	if target == "" {
		return m
	}
	runs, err := history.List(target, logsPaneLimit)
	if err != nil || len(runs) == 0 {
		return m
	}
	idx := m.logsPaneCursor
	if idx < 0 || idx >= len(runs) {
		idx = 0
	}
	run := runs[idx]
	m.logsDetail = &run
	m.logsDetailBtn = target
	m.logsDetailScroll = 0
	return m
}

// handleArgFormKey dispatches a key to the inline form and, on
// submit, validates through button.ParsePressArgs and fires the
// press using the CLI's exact validation path. On cancel, the form
// clears and the board returns to normal.
func (m Model) handleArgFormKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	res, values := m.argForm.handleKey(msg)
	switch res {
	case argFormCancel:
		m.argForm = nil
		return m, nil

	case argFormSubmit:
		target := m.argForm.btnName
		// Find the button (it might have been deleted between form
		// open and submit — unlikely, but an auto-refresh in another
		// terminal is possible).
		var btn *button.Button
		for i := range m.buttons {
			if m.buttons[i].Name == target {
				btn = &m.buttons[i]
				break
			}
		}
		if btn == nil {
			m.argForm = nil
			m.lastErr = fmt.Sprintf("%s is no longer available", target)
			return m, nil
		}
		parsed, err := button.ParsePressArgs(toArgList(values), btn.Args)
		if err != nil {
			// Stay on the form; surface the validation error inline.
			m.argForm.lastErr = err.Error()
			return m, nil
		}
		// Dismiss the form, fire the press with the typed args.
		m.argForm = nil
		return m.pressButtonWithArgs(target, parsed)
	}
	return m, nil
}

// toArgList flattens map[name]value into the key=value slice shape
// button.ParsePressArgs expects — letting us reuse the same
// validator the CLI uses.
func toArgList(values map[string]string) []string {
	out := make([]string, 0, len(values))
	for k, v := range values {
		out = append(out, fmt.Sprintf("%s=%s", k, v))
	}
	return out
}

// moveCursor shifts the grid cursor by delta cells with wrap. Used for
// left/right (±1) and tab (forward).
func (m Model) moveCursor(delta int) tea.Model {
	n := len(m.buttons)
	if n == 0 {
		return m
	}
	m.cursor = ((m.cursor+delta)%n + n) % n
	return m
}

// moveByRow shifts the cursor up/down by one grid row — which is
// cardsPerRow() cells away. Clamps so the cursor never lands on a
// non-existent cell past the end of the last row.
func (m Model) moveByRow(delta int) tea.Model {
	n := len(m.buttons)
	if n == 0 {
		return m
	}
	cols := m.cardsPerRow()
	target := m.cursor + delta*cols
	if target < 0 {
		target = m.cursor % cols
	}
	if target >= n {
		// Same column, last row. If that column is out of range in the
		// last (possibly partial) row, fall back to the last card.
		col := m.cursor % cols
		last := ((n - 1) / cols) * cols
		target = last + col
		if target >= n {
			target = n - 1
		}
	}
	m.cursor = target
	return m
}

// handleLeftClick routes a terminal click to the right action using the
// layout map computed the same way View() composes the screen. Click
// targets (in order of priority): pinned cards, list rows, footer press.
// Footer has no quit hitbox — board is an ambient dashboard, not a
// prompt you dismiss.
func (m Model) handleLeftClick(x, y int) (tea.Model, tea.Cmd) {
	l := m.computeLayout()

	if y >= l.footerY0 && y <= l.footerY1 {
		if l.pressEnabled && x >= l.pressX0 && x < l.pressX1 {
			name := m.currentButtonName()
			if name == "" {
				return m, nil
			}
			return m.pressButton(name)
		}
		return m, nil
	}

	order := m.cardOrder()
	if len(order) > 0 && y >= l.cardsY0 && y <= l.cardsY1 {
		idx := m.cardIndexAt(x, y, l.cardsY0)
		if idx >= 0 {
			m.cursor = idx
			return m.pressButton(order[idx].Name)
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

	// Any required args without values → open the inline form and
	// return to let the user fill them in. The press fires when the
	// form submits (see handlePressFormSubmit).
	hasRequired := false
	for _, a := range btn.Args {
		if a.Required {
			hasRequired = true
			break
		}
	}
	if hasRequired {
		m.argForm = newArgForm(btn)
		m.lastErr = ""
		m.lastOK = ""
		return m, nil
	}

	return m.pressButtonWithArgs(name, nil)
}

// pressButtonWithArgs dispatches a press with pre-resolved args (or
// nil for "no args"). Callers are expected to have already verified
// required-arg validation — either because there are none, or because
// the form just finished collecting values.
func (m Model) pressButtonWithArgs(name string, args map[string]string) (tea.Model, tea.Cmd) {
	// Repeat the running-check here so direct calls are safe too.
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

	m.lastErr = ""
	m.lastOK = ""
	m.status[name] = statusRunning
	m.pressStartedAt[name] = time.Now()

	codePath, _ := m.svc.CodePath(name)

	// Load batteries on each press so a battery added in another shell
	// shows up without restarting the TUI. Silent fallback to empty env
	// if resolution fails — the TUI can't usefully surface a batteries
	// read error mid-press, and the user can always hit the CLI for a
	// full error report.
	batteries := map[string]string{}
	if batSvc, err := battery.NewServiceFromEnv(tuiBatteryDiscoverer); err == nil {
		if env, err := batSvc.Env(); err == nil {
			batteries = env
		}
	}

	pressCmd := runPress(btn, codePath, batteries, args)

	// Four-frame choreography: rest → keydown (now) → fire (+40ms) →
	// release (+180ms). pressPulse is set synchronously so the very
	// next View() already shows the keydown frame; fire and release
	// arrive via tea.Tick.
	m.pressPulse = name
	m.pressPulseFire = false
	fireCmd := tea.Tick(pressFireDelay, func(_ time.Time) tea.Msg { return pressFireMsg{name: name} })
	releaseCmd := tea.Tick(pressReleaseDelay, func(_ time.Time) tea.Msg { return pressReleaseMsg{name: name} })

	if !m.ticking {
		m.ticking = true
		return m, tea.Batch(pressCmd, fireCmd, releaseCmd, tickCmd())
	}
	return m, tea.Batch(pressCmd, fireCmd, releaseCmd)
}

func runPress(btn *button.Button, codePath string, batteries, args map[string]string) tea.Cmd {
	name := btn.Name
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(btn.TimeoutSeconds)*time.Second)
		defer cancel()

		// Board presses don't stream yet — the dedicated `buttons logs`
		// viewer (C2) will pass a sink for live tailing.
		result := engine.Execute(ctx, btn, args, batteries, nil, codePath)
		return pressDoneMsg{name: name, result: result}
	}
}

// tuiBatteryDiscoverer is the project-dir discoverer passed to
// battery.NewServiceFromEnv — shared shape with the CLI helper but
// defined here so the tui package doesn't reach into cmd.
func tuiBatteryDiscoverer() (string, bool) {
	if !config.IsProjectLocal() {
		return "", false
	}
	dir, err := config.DataDir()
	if err != nil {
		return "", false
	}
	return dir, true
}

func (m Model) handlePressDone(msg pressDoneMsg) Model {
	// Stale elapsed entry would linger as "active · 83s" on a row that
	// just finished. Clear it the instant we know the press terminated.
	delete(m.pressStartedAt, msg.name)

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
	cardsY0, cardsY1   int
	footerY0, footerY1 int
	pressX0, pressX1   int
	pressEnabled       bool
}

const (
	leftPad = 2
	// cardHeightIdle is the rendered height of an idle card: top border
	// + name line + meta line + bottom border = 4 rows.
	cardHeightIdle = 4
	// cardGutter is the vertical blank between card grid rows.
	cardGutter    = 1
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
		hero := m.renderEmptyHero()
		y += countLines(hero) + sectionBlank + dividerHeight + sectionBlank
	} else {
		cols := m.cardsPerRow()
		rows := (len(m.buttons) + cols - 1) / cols
		// Each row = card height + gutter between rows. Active cards
		// are 1 taller (badge above) — but the layout math just needs
		// a rough bound for hit-testing; hit-test accepts the larger
		// Y1.
		rowHeight := cardHeightIdle + cardGutter
		gridHeight := rows*rowHeight - cardGutter
		if gridHeight < cardHeightIdle {
			gridHeight = cardHeightIdle
		}
		l.cardsY0 = y
		l.cardsY1 = y + gridHeight - 1
		y = l.cardsY1 + 1 + sectionBlank + dividerHeight + sectionBlank
	}

	l.footerY0 = y
	l.footerY1 = y + footerHeight - 1

	// Press pill width = label("● press" = 7) + padding(6) + border(2).
	pressW := 7 + 6 + 2
	l.pressX0 = leftPad
	l.pressX1 = l.pressX0 + pressW

	l.pressEnabled = m.currentButtonName() != ""

	return l
}

// cardsPerRow returns how many cards fit on a single grid row given
// the current terminal width and the longest button name (cards share
// a common width so the grid aligns).
func (m Model) cardsPerRow() int {
	w := m.width
	if w <= 0 {
		w = 80
	}
	cardW := m.cardOuterWidth()
	avail := w - leftPad*2
	if avail < cardW {
		return 1
	}
	// cardW + 2 accounts for the 2-space gutter between cards; + 2 in
	// the numerator makes the divisor fit exactly when the row ends
	// flush against the right margin without a trailing gutter.
	cols := (avail + 2) / (cardW + 2)
	if cols < 1 {
		cols = 1
	}
	return cols
}

// cardOuterWidth is the rendered width of a single card: inner label
// width + padding(4) + border(2). The inner label is normalized to the
// longest name in the grid so all cards align.
func (m Model) cardOuterWidth() int {
	return m.maxNameWidth() + 6
}

// maxNameWidth returns the display width to pad every card's name
// line to. Floors at 10 so short grids don't produce claustrophobic
// cards.
func (m Model) maxNameWidth() int {
	w := 10
	for _, b := range m.buttons {
		if n := lipgloss.Width(b.Name); n > w {
			w = n
		}
	}
	return w
}

// cardIndexAt converts a click at (x, y) inside the card-grid region
// into the index of the card that was clicked, or -1 if the click
// landed in a gutter. y0 is the row where the grid starts.
func (m Model) cardIndexAt(x, y, y0 int) int {
	order := m.cardOrder()
	if len(order) == 0 {
		return -1
	}
	cols := m.cardsPerRow()
	rowHeight := cardHeightIdle + cardGutter
	row := (y - y0) / rowHeight
	// Reject clicks inside the gutter between rows.
	if (y-y0)%rowHeight >= cardHeightIdle {
		return -1
	}
	cardW := m.cardOuterWidth()
	if x < leftPad {
		return -1
	}
	col := (x - leftPad) / (cardW + 2)
	if (x-leftPad)%(cardW+2) >= cardW {
		return -1
	}
	idx := row*cols + col
	if idx < 0 || idx >= len(order) {
		return -1
	}
	return idx
}

// ------------------------------------------------------------------
// View
// ------------------------------------------------------------------

func (m Model) View() tea.View {
	var parts []string

	parts = append(parts, m.renderHeader())
	parts = append(parts, m.renderDivider())
	parts = append(parts, "") // blank before content

	switch {
	case m.logsDetail != nil:
		// Detail view takes the full center column; other chrome
		// (header / divider / footer) still renders so the user
		// stays oriented.
		parts = append(parts, m.renderLogDetail())
	case m.argForm != nil:
		// Modal-ish content: form replaces the grid / hero block but
		// the board's header + divider + footer scaffold stays so the
		// user never loses their visual orientation.
		parts = append(parts, m.argForm.render(m.styles, m.width))
	case len(m.buttons) == 0:
		parts = append(parts, m.renderEmptyHero())
	default:
		parts = append(parts, m.renderCards())
	}

	if m.argForm == nil && m.logsDetail == nil && m.logsOpen && len(m.buttons) > 0 {
		parts = append(parts, "")
		parts = append(parts, m.renderDivider())
		parts = append(parts, "")
		parts = append(parts, m.renderLogs())
	}

	parts = append(parts, "")
	parts = append(parts, m.renderDivider())
	parts = append(parts, "")
	parts = append(parts, m.renderFooter())

	if status := m.renderStatus(); status != "" {
		parts = append(parts, "")
		parts = append(parts, status)
	}

	// Bottom status chrome — tiny muted strip with contextual badges.
	// Matches the spec stations' chrome row (TTY 1 · UTF-8 · 256-COLOR
	// · ● REC / ● ACTIVE / LOGS OPEN / etc.).
	parts = append(parts, "")
	parts = append(parts, m.renderChrome())

	content := strings.Join(parts, "\n")

	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m Model) renderHeader() string {
	// Brand mark: always-visible orange dot to the left of the wordmark.
	// This is the identity spec's header element — "the buttons logo."
	// Not an active-state indicator; it stays even when nothing's
	// running. Use of orange here is the one legitimate "orange isn't
	// for ACTIVE only" exception in the codebase.
	left := m.styles.BrandDot.Render("● ") + m.styles.Wordmark.Render("buttons")
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

// renderCards draws the full card grid. Every button renders as a
// bordered box; the grid wraps to multiple rows based on terminal
// width. Pinned buttons sort to the front via cardOrder().
func (m Model) renderCards() string {
	order := m.cardOrder()
	if len(order) == 0 {
		return ""
	}
	cols := m.cardsPerRow()

	rows := make([]string, 0)
	for i := 0; i < len(order); i += cols {
		end := i + cols
		if end > len(order) {
			end = len(order)
		}
		cells := make([]string, 0, (end-i)*2)
		for j, btn := range order[i:end] {
			if j > 0 {
				cells = append(cells, "  ")
			}
			cells = append(cells, m.renderCard(btn, i+j == m.cursor))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, cells...))
	}

	// Blank line between grid rows keeps cards feeling like distinct
	// objects rather than a packed contact sheet.
	return indentBlock(strings.Join(rows, "\n\n"), leftPad)
}

// renderCard paints one bordered button cell. Layout is two interior
// lines: name on top, meta ("SHELL · 300S") or elapsed toast beneath.
// Active presses get a "↵ TAIL" badge floating above the top-right
// corner so users know ↵ will open the live log stream.
func (m Model) renderCard(btn button.Button, selected bool) string {
	// State → style priority: running > fire pulse > keydown pulse >
	// selected > idle. Matches the logic that was on renderPinnedCard
	// so a long press doesn't flicker back to idle once the 180ms
	// release timer fires.
	style := m.styles.PinnedIdle
	switch {
	case m.status[btn.Name] == statusRunning:
		style = m.styles.PinnedActive
	case m.pressPulse == btn.Name && m.pressPulseFire:
		style = m.styles.PinnedActive
	case m.pressPulse == btn.Name:
		style = m.styles.PinnedSelected
	case selected:
		style = m.styles.PinnedSelected
	}

	nameWidth := m.maxNameWidth()
	name := btn.Name
	if lipgloss.Width(name) < nameWidth {
		name = fmt.Sprintf("%-*s", nameWidth, name)
	}

	var sub string
	if m.status[btn.Name] == statusRunning {
		sub = m.styles.Indicator.Render("● ACTIVE") +
			m.styles.Muted.Render(" · "+formatElapsed(m.elapsedFor(btn.Name)))
	} else {
		sub = m.styles.Muted.Render(cardMeta(btn))
	}

	// Center-align the sub line within the card's interior width so
	// short meta strings (e.g., "HTTP · 60S") sit under the name
	// instead of hard-left against the border.
	card := style.Render(name + "\n" + sub)

	if m.status[btn.Name] == statusRunning {
		cardW := lipgloss.Width(card)
		badge := m.styles.BadgeActive.Render("↵ TAIL")
		offset := cardW - lipgloss.Width(badge)
		if offset < 0 {
			offset = 0
		}
		return strings.Repeat(" ", offset) + badge + "\n" + card
	}
	return card
}

// cardMeta is the tiny caption printed on the second interior line of
// each card — "SHELL · 300S", "HTTP · 60S". Upper-cased so it reads as
// a chip, not a sentence.
func cardMeta(b button.Button) string {
	rt := strings.ToUpper(b.Runtime)
	if rt == "" {
		rt = "SHELL"
	}
	return fmt.Sprintf("%s · %ds", rt, b.TimeoutSeconds)
}

// elapsedFor returns how long the press has been running, or 0 when
// there's no active press for the name (or the press just finished and
// the entry was cleared). The caller is expected to check status
// before deciding whether to render.
func (m Model) elapsedFor(name string) time.Duration {
	start, ok := m.pressStartedAt[name]
	if !ok {
		return 0
	}
	return time.Since(start)
}

// formatElapsed renders a duration compactly:
//
//	< 60s      → "3.2s"   (one decimal — feels live)
//	< 60min    → "2:07"   (mm:ss — feels long)
//	>= 60min   → "1:04:22" (hh:mm:ss — rare but tidy)
//
// Picked for glanceability: short presses look responsive, longer
// presses read as a clock.
func formatElapsed(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) - m*60
		return fmt.Sprintf("%d:%02d", m, s)
	}
	h := int(d.Hours())
	mins := int(d.Minutes()) - h*60
	s := int(d.Seconds()) - h*3600 - mins*60
	return fmt.Sprintf("%d:%02d:%02d", h, mins, s)
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
		m.styles.Muted.Render("run the commands above in another terminal — this board updates automatically."),
	}

	hero := strings.Join(lines, "\n")
	return indentBlock(hero, leftPad*2)
}

// renderFooter shows only the primary press action and a minimal
// nav / press hint line. The board is ambient UI — it has no "quit"
// concept. Uses lipgloss.JoinHorizontal so the multi-line bordered
// press box aligns with the hints placed on its middle baseline.
func (m Model) renderFooter() string {
	pressStyle := m.styles.ActionPrimary
	if !m.pressIsEnabled() {
		pressStyle = m.styles.ActionPrimaryDisabled
	}
	press := pressStyle.Render("● press")

	hints := m.composeHints()

	w := m.width
	if w <= 0 {
		w = 80
	}
	pressW := lipgloss.Width(press)
	hintsW := lipgloss.Width(hints)
	gap := w - pressW - hintsW - leftPad*2
	if gap < 2 {
		gap = 2
	}

	hintBlock := lipgloss.Place(hintsW, lipgloss.Height(press), lipgloss.Left, lipgloss.Center, hints)
	row := lipgloss.JoinHorizontal(lipgloss.Top, press, strings.Repeat(" ", gap), hintBlock)

	return indentBlock(row, leftPad)
}

// composeHints renders the minimal keybind legend shown in the footer.
// No quit chip — board is an ambient dashboard, not a prompt to dismiss.
// The logs chip flips label between `logs` and `hide` based on state so
// the hint always reads as the action the key will take.
func (m Model) composeHints() string {
	pair := func(key, label string) string {
		return m.styles.KeyChip.Render(key) + m.styles.Muted.Render(" "+label)
	}
	sep := m.styles.Muted.Render("  ·  ")

	// Hints flip to match the mode the user is currently in. In the
	// detail view they need to know scroll + back; in pane mode they
	// need ↵ = details, not press. Keeps the chip row honest.
	if m.logsDetail != nil {
		return pair("↑↓", "scroll") + sep + pair("esc", "back")
	}
	if m.logsOpen {
		return pair("↑↓", "run") + sep + pair("↵", "details") + sep + pair("L", "hide") + sep + pair("esc", "close")
	}
	return pair("↑↓", "nav") + sep + pair("↵", "press") + sep + pair("L", "logs") + sep + pair("?", "help")
}

// renderChrome is the bottom status strip that mirrors the spec
// mockup chrome ("TTY 1 · UTF-8 · 256-COLOR · ● REC"). Contextual
// badges on the right flip based on what the board is doing right
// now — active press, logs pane open, arg form open — so the chrome
// reads as the board's current mode-line.
func (m Model) renderChrome() string {
	left := strings.Join([]string{"TTY 1", "UTF-8", "256-COLOR"}, m.styles.Muted.Render(" · "))
	left = m.styles.Chrome.Render(left)

	right := ""
	// Active press wins the badge: show "● ACTIVE · <name>". If no
	// press, surface the next most-informative state.
	switch {
	case m.anyRunning():
		for name, s := range m.status {
			if s == statusRunning {
				right = m.styles.ChromeActiveBadge.Render("● ACTIVE") +
					m.styles.Muted.Render(" · ") +
					m.styles.Chrome.Render(name)
				break
			}
		}
	case m.logsDetail != nil:
		right = m.styles.Chrome.Render("DETAIL · " + m.logsDetailBtn)
	case m.argForm != nil:
		right = m.styles.Chrome.Render(fmt.Sprintf("ARGS %d · DRY-RUN READY", len(m.argForm.fields)))
	case m.logsOpen:
		target := m.currentButtonName()
		if target == "" {
			target = "none"
		}
		right = m.styles.Chrome.Render("LOGS OPEN · SCOPE " + target)
	default:
		right = m.styles.ChromeActiveBadge.Render("● REC")
	}

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

// renderLogs renders the collapsible history pane that sits above the
// footer when `l` has been toggled on. Scope is the button currently
// under the cursor — users looking at a row expect to see its runs,
// not a global mix.
//
// Each row reads: glyph · time · exit · duration · preview. Preview is
// the first non-empty line of stdout (or stderr when the press failed),
// trimmed so the row always fits one terminal line.
func (m Model) renderLogs() string {
	title := m.styles.HeroTitle.Render("logs")
	target := m.currentButtonName()
	if target == "" {
		empty := m.styles.Muted.Render("focus a button to see its history")
		return indentBlock(title+"\n\n"+empty, leftPad)
	}

	runs, err := history.List(target, logsPaneLimit)
	if err != nil || len(runs) == 0 {
		empty := m.styles.Muted.Render(
			fmt.Sprintf("no runs for %s yet — press it to record one.", target),
		)
		return indentBlock(title+m.styles.Muted.Render("  ·  "+target)+"\n\n"+empty, leftPad)
	}

	lines := []string{
		title + m.styles.Muted.Render(fmt.Sprintf("  ·  %s  ·  last %d", target, len(runs))),
		"",
	}
	// Width budget for the stdout/stderr preview: terminal width minus
	// the indent and the fixed-width columns we render before it. Keep
	// a minimum so very narrow terminals still show something useful.
	previewBudget := m.width - leftPad - 2 /* row indent */ - 38 /* glyph+time+exit+dur */
	if previewBudget < 20 {
		previewBudget = 20
	}
	cursor := m.logsPaneCursor
	if cursor >= len(runs) {
		cursor = len(runs) - 1
	}
	for i, r := range runs {
		lines = append(lines, m.renderLogRow(r, previewBudget, i == cursor))
	}
	// Footer hint tells the user ↵ is now bound to "details" instead
	// of "press" — the same chip the bottom hint row surfaces, echoed
	// here so pane focus is unambiguous.
	lines = append(lines, "",
		m.styles.Muted.Render("  ↵ open details   esc close"),
	)
	return indentBlock(strings.Join(lines, "\n"), leftPad)
}

func (m Model) renderLogRow(r history.Run, previewBudget int, selected bool) string {
	var glyph string
	switch r.Status {
	case "ok":
		glyph = m.styles.StatusOK.Render("✓")
	default:
		glyph = m.styles.StatusError.Render("✗")
	}

	localTime := r.StartedAt.Local().Format("15:04:05")
	meta := fmt.Sprintf("exit %-3d  %5dms", r.ExitCode, r.DurationMs)

	// Prefer stdout for successful runs, stderr for failed ones. First
	// non-empty line only — stops the row from consuming the pane with a
	// long multi-line dump.
	source := r.Stdout
	if r.Status != "ok" && r.Stderr != "" {
		source = r.Stderr
	}
	preview := firstLineTrimmed(source)
	preview = truncateDisplay(preview, previewBudget)

	preview = m.styles.Secondary.Render(preview)
	timeCol := m.styles.Muted.Render(localTime)
	metaCol := m.styles.Muted.Render(meta)

	cur := "  "
	if selected {
		cur = m.styles.ButtonNameSelected.Render("› ")
	}
	return fmt.Sprintf("%s%s  %s  %s  %s", cur, glyph, timeCol, metaCol, preview)
}

// renderLogDetail is the full-screen historical-run view shown when
// the user hits ↵ on a pane row. Layout: identity row + metadata row
// + streamed body (stdout, then stderr if any). Scrolls via up/down
// while the detail is open.
func (m Model) renderLogDetail() string {
	if m.logsDetail == nil {
		return ""
	}
	r := m.logsDetail

	// Identity row — button name on the left, run timestamp on the
	// right. Matches the logs view's header rhythm so users switching
	// between live and historical views never feel lost.
	name := m.styles.Wordmark.Render(m.logsDetailBtn)
	press := m.styles.Muted.Render("  ·  " + r.StartedAt.Local().Format("2006-01-02 15:04:05"))
	left := name + press

	var statusCol string
	if r.Status == "ok" {
		statusCol = m.styles.StatusOK.Render(fmt.Sprintf("✓ exit %d · %dms", r.ExitCode, r.DurationMs))
	} else {
		statusCol = m.styles.StatusError.Render(fmt.Sprintf("✗ %s · exit %d · %dms", r.Status, r.ExitCode, r.DurationMs))
	}
	right := statusCol

	w := m.width
	if w <= 0 {
		w = 80
	}
	gap := w - visibleLen(left) - visibleLen(right) - leftPad*2
	if gap < 2 {
		gap = 2
	}
	header := strings.Repeat(" ", leftPad) + left + strings.Repeat(" ", gap) + right

	// Body — stdout first, then stderr if present. Both are shown
	// unmodified (aside from styling) so copy-paste into a bug report
	// round-trips cleanly. Each section gets a muted label.
	bodyLines := []string{}
	if strings.TrimSpace(r.Stdout) != "" {
		bodyLines = append(bodyLines, m.styles.Muted.Render("─ stdout ─"))
		for _, line := range strings.Split(strings.TrimRight(r.Stdout, "\n"), "\n") {
			bodyLines = append(bodyLines, m.styles.ButtonName.Render(line))
		}
	}
	if strings.TrimSpace(r.Stderr) != "" {
		if len(bodyLines) > 0 {
			bodyLines = append(bodyLines, "")
		}
		bodyLines = append(bodyLines, m.styles.Muted.Render("─ stderr ─"))
		for _, line := range strings.Split(strings.TrimRight(r.Stderr, "\n"), "\n") {
			bodyLines = append(bodyLines, m.styles.StatusError.Render(line))
		}
	}
	if len(bodyLines) == 0 {
		bodyLines = append(bodyLines, m.styles.Muted.Render("(no output recorded)"))
	}

	// Clamp scroll so we never render past the end of the body. Keep
	// at least one line in view — scrolling into an empty tail makes
	// the pane feel broken.
	visibleHeight := m.height - 10
	if visibleHeight < 5 {
		visibleHeight = 5
	}
	scroll := m.logsDetailScroll
	maxScroll := len(bodyLines) - visibleHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if scroll > maxScroll {
		scroll = maxScroll
	}
	if scroll < 0 {
		scroll = 0
	}
	end := scroll + visibleHeight
	if end > len(bodyLines) {
		end = len(bodyLines)
	}
	body := indentBlock(strings.Join(bodyLines[scroll:end], "\n"), leftPad*2)

	return header + "\n" + m.renderDivider() + "\n\n" + body
}

// firstLineTrimmed returns the first non-empty line of s with leading/
// trailing whitespace removed. Empty input returns "(no output)" so the
// logs row never renders as just a blank gap.
func firstLineTrimmed(s string) string {
	for _, raw := range strings.Split(s, "\n") {
		line := strings.TrimSpace(raw)
		if line != "" {
			return line
		}
	}
	return "(no output)"
}

// truncateDisplay shortens s to fit within `cells` terminal cells,
// appending an ellipsis when it had to cut. Rune-aware so emoji / wide
// chars don't get sliced mid-codepoint.
func truncateDisplay(s string, cells int) string {
	if cells <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= cells {
		return s
	}
	// Lip Gloss doesn't ship a truncate helper we can rely on across
	// versions; walk runes, summing widths.
	var b strings.Builder
	used := 0
	for _, r := range s {
		w := lipgloss.Width(string(r))
		if used+w+1 > cells { // reserve 1 cell for the ellipsis
			break
		}
		b.WriteRune(r)
		used += w
	}
	return b.String() + "…"
}

// renderStatus returns the single-line status/toast below the footer,
// or "" if there's nothing to say. Errors read red-indicator, successes
// read quieter so they don't compete visually.
func (m Model) renderStatus() string {
	// Active-press toast — spec prints this in orange with a pulsing
	// dot, pointing at where the output is being streamed.
	if m.anyRunning() {
		for name, s := range m.status {
			if s == statusRunning {
				msg := fmt.Sprintf("● %s running… stdout streaming to ~/.buttons/buttons/%s/pressed/", name, name)
				return strings.Repeat(" ", leftPad) + m.styles.StatusRunning.Render(msg)
			}
		}
	}
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

// cardOrder returns m.buttons with pinned entries floated to the
// front while preserving each bucket's original order. The cursor
// indexes into this slice, not m.buttons directly.
func (m Model) cardOrder() []button.Button {
	if len(m.buttons) == 0 {
		return nil
	}
	out := make([]button.Button, 0, len(m.buttons))
	for _, b := range m.buttons {
		if b.Pinned {
			out = append(out, b)
		}
	}
	for _, b := range m.buttons {
		if !b.Pinned {
			out = append(out, b)
		}
	}
	return out
}

func (m Model) currentButtonName() string {
	order := m.cardOrder()
	if len(order) == 0 {
		return ""
	}
	idx := m.cursor
	if idx < 0 || idx >= len(order) {
		idx = 0
	}
	return order[idx].Name
}

func (m *Model) focusByName(name string) {
	order := m.cardOrder()
	for i, b := range order {
		if b.Name == name {
			m.cursor = i
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

