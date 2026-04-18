# Implementing the Buttons TUI

> A working guide for the `buttons board` TUI. Everything in this doc maps
> to a real symbol in `internal/tui/`. If you're here to add a new screen,
> keep a terminal open on `internal/tui/board.go` and `internal/tui/styles.go`
> — you'll be editing them.

---

## Contents

1. [Architecture in 30 seconds](#architecture-in-30-seconds)
2. [The identity rule, encoded](#the-identity-rule-encoded)
3. [Lip Gloss style system (`styles.go`)](#lip-gloss-style-system-stylesgo)
4. [The Bubble Tea model (`board.go`)](#the-bubble-tea-model-boardgo)
5. [Input: keyboard, mouse, and layout hit-testing](#input-keyboard-mouse-and-layout-hit-testing)
6. [Pressing buttons](#pressing-buttons)
7. [Mechanical press choreography](#mechanical-press-choreography)
8. [The logs view](#the-logs-view)
9. [Themes (paper · phosphor · amber)](#themes-paper--phosphor--amber)
10. [Testing the TUI](#testing-the-tui)
11. [Anti-patterns we've rejected](#anti-patterns-weve-rejected)

---

## Architecture in 30 seconds

Bubble Tea v2 + Lip Gloss v2, Elm-style:

```
┌─────────────┐      KeyPressMsg       ┌───────────────┐
│   tea.Program  │ ─────────────────▶ │  Model.Update │
│ (event loop)   │                    │  (pure)       │
└─────────────┘      new Model+Cmd    └───────────────┘
      ▲                                       │
      │              Model.View()             │
      │                                       ▼
      │                              ┌───────────────┐
      └──────────────────────────────│ Lip Gloss     │
                      string frame   │ style.Render()│
                                     └───────────────┘
```

Three files, three jobs:

| File                     | Job                                                                 |
|--------------------------|---------------------------------------------------------------------|
| `internal/tui/app.go`    | Thin `Run()` entry — constructs the model, wires `tea.NewProgram`.  |
| `internal/tui/styles.go` | Every Lip Gloss style, built once at startup from the identity spec.|
| `internal/tui/board.go`  | The entire Elm triple: `Model`, `Init`, `Update`, `View`.           |

Bubble Tea v2 changes worth knowing:

- **Alt-screen and mouse mode live on the `tea.View`**, not `tea.NewProgram`.
  Set `v.AltScreen = true` and `v.MouseMode = tea.MouseModeCellMotion`
  inside `View()`.
- **`tea.KeyPressMsg` and `tea.KeyReleaseMsg` are distinct.** We only
  handle press. Release is reserved for the mechanical-press work
  described later.
- **`tea.MouseClickMsg` carries `.Mouse()` returning X/Y/button.**
  Cell-motion mouse mode gives us hover coordinates too.

---

## The identity rule, encoded

From the spec, baked into `styles.go` as a comment and a constraint:

```go
// Hard rule from the identity spec: orange (Indicator) is only used to
// signal an active/running state. If you find yourself reaching for it
// for decoration, don't.
const (
    hexInk       = "#0A0A0A"
    hexIndicator = "#FF5A1F" // reserved for ACTIVE
    hexPaper     = "#F5F5F2"
    hexAluminum  = "#C5C5C0"
    hexDust      = "#6E6E68"
)
```

If you add a new style and it uses `colorIndicator` for anything that
isn't "a press is running right now", you've broken the rule. The
spinner, the `ButtonNameActive`, the `BadgeActive`, the `PinnedActive`
card, and the `StatusError` line are the only legitimate users.

> `StatusError` uses indicator because in the Buttons worldview, a
> failure *is* an active concern — something needs your attention. This
> is the one edge where "active" stretches beyond "running".

---

## Lip Gloss style system (`styles.go`)

### Build once, render many

All styles are assembled at startup by `BuildStyles()` and stashed on
the model:

```go
type Model struct {
    svc    *button.Service
    styles Styles
    // ...
}

func New(svc *button.Service, initial string) (*Model, error) {
    m := &Model{
        svc:    svc,
        styles: BuildStyles(),
        // ...
    }
    return m, nil
}
```

Why: Lip Gloss style objects are immutable value types but allocating
them per-render burns CPU on big lists. Build once; every `View()` call
just calls `.Render(string)`.

### Adaptive color via `lipgloss.LightDark`

```go
func BuildStyles() Styles {
    hasDark := lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
    ld := lipgloss.LightDark(hasDark)

    colorPrimary   := ld(lipgloss.Color(hexInk),    lipgloss.Color(hexPaper))
    colorSecondary := ld(lipgloss.Color(hexDust),   lipgloss.Color("#A8A8A0"))
    colorMuted     := ld(lipgloss.Color(hexAluminum), lipgloss.Color("#3A3A38"))
    // ...
}
```

`ld(lightValue, darkValue)` returns the right color for the detected
terminal. **Detection happens once** — we don't poll. If the user
switches their terminal theme mid-session, they'll need to restart.
That's fine; it's a terminal.

### The Styles struct

Every style the board uses, named by role not by appearance:

```go
type Styles struct {
    Wordmark  lipgloss.Style  // the "buttons" logo
    Label     lipgloss.Style
    Divider   lipgloss.Style
    Secondary lipgloss.Style
    Muted     lipgloss.Style
    Indicator lipgloss.Style

    ButtonName         lipgloss.Style
    ButtonNameSelected lipgloss.Style
    ButtonNameActive   lipgloss.Style

    BadgeActive           lipgloss.Style
    ActionPrimary         lipgloss.Style  // the press pill
    ActionSecondary       lipgloss.Style
    ActionPrimaryDisabled lipgloss.Style
    KeyChip               lipgloss.Style  // the ↑↓/↵ glyphs

    PinnedIdle     lipgloss.Style  // pinned card, default
    PinnedSelected lipgloss.Style  // pinned card, cursor on it
    PinnedActive   lipgloss.Style  // pinned card, press running

    StatusError lipgloss.Style
    StatusOK    lipgloss.Style
}
```

Name by *what it represents*, not *what it looks like*. Today
`ButtonNameActive` is orange-and-bold; in a future theme it might be
cyan-and-underlined. The code shouldn't change.

### The three pinned-card states

These compose the whole mechanical-button illusion — same three
primitives, three border treatments:

```go
PinnedIdle: lipgloss.NewStyle().
    Foreground(colorPrimary).
    Border(lipgloss.NormalBorder()).
    BorderForeground(colorMuted).
    Padding(1, 3).
    Align(lipgloss.Center),

PinnedSelected: lipgloss.NewStyle().
    Foreground(colorPrimary).
    Bold(true).
    Border(lipgloss.ThickBorder()).       // ← thicker border = "focused"
    BorderForeground(colorPrimary).
    Padding(1, 3).
    Align(lipgloss.Center),

PinnedActive: lipgloss.NewStyle().
    Foreground(colorIndicator).
    Bold(true).
    Border(lipgloss.ThickBorder()).       // ← thick + orange = "running"
    BorderForeground(colorIndicator).
    Padding(1, 3).
    Align(lipgloss.Center),
```

`NormalBorder` → `ThickBorder` is the *entire* physical-press metaphor.
It costs you nothing extra at render time.

### The spinner

```go
var spinnerFrames = []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}

func (s Styles) indicator(active bool, frame int) string {
    if active {
        if frame >= 0 {
            return s.Indicator.Render(string(spinnerFrames[frame%len(spinnerFrames)]))
        }
        return s.Indicator.Render("■")
    }
    return s.Muted.Render("□")
}
```

Standard Charm braille cycle. The `frame` argument comes from the
`spinnerFrame` field on the model, incremented on every `tickMsg`.

---

## The Bubble Tea model (`board.go`)

### Model shape

```go
type Model struct {
    svc    *button.Service
    styles Styles

    buttons []button.Button

    section      section       // pinned row or list
    cursorPinned int
    cursorList   int

    status map[string]runStatus // per-button run state

    lastErr string
    lastOK  string              // transient success toast

    spinnerFrame int
    ticking      bool

    logsOpen bool                // L toggle

    width, height int
}

type runStatus int
const (
    statusIdle runStatus = iota
    statusRunning
    statusOK
    statusFailed
)
```

### The two tick loops

Two kinds of periodic message, both implemented with `tea.Tick`:

```go
type tickMsg    time.Time // spinner, 90ms
type refreshMsg time.Time // disk re-list, 2s

const (
    tickInterval    = 90 * time.Millisecond
    refreshInterval = 2 * time.Second
)

func tickCmd() tea.Cmd {
    return tea.Tick(tickInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func refreshCmd() tea.Cmd {
    return tea.Tick(refreshInterval, func(t time.Time) tea.Msg { return refreshMsg(t) })
}
```

Key optimization in `Update` — **don't keep ticking when nothing is
running**:

```go
case tickMsg:
    m.spinnerFrame++
    if m.anyRunning() {
        return m, tickCmd()   // reschedule
    }
    m.ticking = false          // flatline when idle
    return m, nil
```

And in `pressButton`:

```go
if !m.ticking {
    m.ticking = true
    return m, tea.Batch(pressCmd, tickCmd())
}
return m, pressCmd
```

A board sitting open on an idle project must not cook the CPU.

### The `Init` / `Update` / `View` triple

```go
func (m Model) Init() tea.Cmd { return refreshCmd() }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.WindowSizeMsg:  // track dimensions for layout
        m.width, m.height = msg.Width, msg.Height
        return m, nil
    case tea.KeyPressMsg:    // keyboard
        return m.handleKey(msg)
    case tea.MouseClickMsg:  // mouse
        if msg.Mouse().Button == tea.MouseLeft {
            return m.handleLeftClick(msg.Mouse().X, msg.Mouse().Y)
        }
        return m, nil
    case pressDoneMsg:        // press finished
        return m.handlePressDone(msg), nil
    case tickMsg:             // spinner tick
        m.spinnerFrame++
        if m.anyRunning() { return m, tickCmd() }
        m.ticking = false
        return m, nil
    case refreshMsg:          // disk sync
        if buttons, err := m.svc.List(); err == nil {
            m.buttons = buttons
            if m.cursorList >= len(m.buttons) && len(m.buttons) > 0 {
                m.cursorList = len(m.buttons) - 1
            }
        }
        return m, refreshCmd()
    }
    return m, nil
}
```

`Update` is **value-receiver pure**. Never mutate `m` in place and
return the original — the compiler won't catch it, Bubble Tea won't
complain, but your state updates silently won't stick. Always return
the modified copy.

---

## Input: keyboard, mouse, and layout hit-testing

### Keyboard

```go
func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
    switch msg.String() {
    case "ctrl+c":     return m, tea.Quit
    case "up", "k":    return m.moveCursor(-1), nil
    case "down", "j":  return m.moveCursor(1), nil
    case "left", "h":  /* section-aware: left in pinned row, or hop to pinned from list */
    case "right", "l": /* section-aware: right in pinned row */
    case "tab":        /* swap sections */
    case "enter", " ": return m.pressButton(m.currentButtonName())
    case "L":          m.logsOpen = !m.logsOpen; return m, nil
    }
    return m, nil
}
```

Two things to notice:

- **Lower-case `l` moves cursor right (vim).** The logs toggle gets
  shift+L because its hint chip shows a capital — the glyph the user
  sees on screen *is* the key they need to press.
- **No `q` binding.** The board is an ambient dashboard. Pressing `q`
  to dismiss it would imply it's a prompt you're trapped inside.
  Ctrl+C stays as an unadvertised escape hatch.

### Mouse (cell-motion mode)

The view ends with:

```go
v := tea.NewView(content)
v.AltScreen = true
v.MouseMode = tea.MouseModeCellMotion
return v
```

Cell-motion gives us X/Y per click *and* hover. Click handling reuses
the layout calculation:

```go
func (m Model) handleLeftClick(x, y int) (tea.Model, tea.Cmd) {
    l := m.computeLayout()

    if y >= l.footerY0 && y <= l.footerY1 {
        if l.pressEnabled && x >= l.pressX0 && x < l.pressX1 {
            return m.pressButton(m.currentButtonName())
        }
        return m, nil
    }

    // Pinned row hit test
    pinned := m.pinned()
    if len(pinned) > 0 && y >= l.pinnedY0 && y <= l.pinnedY1 {
        idx := pinnedIndexAtX(pinned, x)
        if idx >= 0 {
            m.section = sectionPinned
            m.cursorPinned = idx
            return m.pressButton(pinned[idx].Name)
        }
    }

    // List row hit test
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
```

### `computeLayout` is the hinge

Bubble Tea doesn't track where it rendered things. `View()` is a
string; the terminal knows nothing. For mouse, **we recompute layout
from model state** the same way `View()` composes the screen — the two
can't drift because they're reading the same inputs:

```go
type layout struct {
    pinnedY0, pinnedY1 int
    listY0, listY1     int
    footerY0, footerY1 int
    pressX0, pressX1   int
    pressEnabled       bool
}

const (
    leftPad       = 2
    pinnedHeight  = 5 // top border + pad + text + pad + bottom border
    footerHeight  = 3 // bordered action pill height
    headerHeight  = 1
    dividerHeight = 1
    sectionBlank  = 1
)

func (m Model) computeLayout() layout {
    l := layout{}
    y := headerHeight + dividerHeight + sectionBlank
    if len(m.buttons) == 0 {
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
    // press pill width = padding(2) + label + padding(2) + border(2)
    pressW := len("press") + 6
    l.pressX0 = leftPad
    l.pressX1 = l.pressX0 + pressW
    l.pressEnabled = m.currentButtonName() != ""
    return l
}
```

If you add a new visible section, **add its Y range to `layout`** and
update `computeLayout` in the same commit that updates `View()`. Mouse
clicks on the new section won't work until you do.

---

## Pressing buttons

The press flow is five steps:

1. **Guard** — one press in flight at a time.
2. **Arg validation** — required args kick the user to the CLI.
3. **Status flip** — `statusRunning` + spinner.
4. **Execute** — `engine.Execute` on a goroutine via `tea.Cmd`.
5. **Resolve** — `pressDoneMsg` returns result or error.

```go
func (m Model) pressButton(name string) (tea.Model, tea.Cmd) {
    // 1. One at a time
    for _, s := range m.status {
        if s == statusRunning { return m, nil }
    }

    // find the button
    var btn *button.Button
    for i := range m.buttons {
        if m.buttons[i].Name == name { btn = &m.buttons[i]; break }
    }
    if btn == nil { return m, nil }

    // 2. Required-arg bail
    for _, a := range btn.Args {
        if a.Required {
            m.lastErr = fmt.Sprintf("%s requires --arg %s; press from the CLI for now", name, a.Name)
            return m, nil
        }
    }

    // 3. Flip status
    m.lastErr, m.lastOK = "", ""
    m.status[name] = statusRunning

    // 4. Execute on a goroutine — tea.Cmd
    codePath, _ := m.svc.CodePath(name)
    batteries := loadBatteries() // best-effort
    pressCmd := runPress(btn, codePath, batteries)

    // Start spinner tick if it isn't already ticking
    if !m.ticking {
        m.ticking = true
        return m, tea.Batch(pressCmd, tickCmd())
    }
    return m, pressCmd
}

func runPress(btn *button.Button, codePath string, batteries map[string]string) tea.Cmd {
    name := btn.Name
    return func() tea.Msg {
        ctx, cancel := context.WithTimeout(context.Background(),
            time.Duration(btn.TimeoutSeconds)*time.Second)
        defer cancel()

        result := engine.Execute(ctx, btn, nil, batteries, codePath)
        return pressDoneMsg{name: name, result: result}
    }
}

// 5. Resolve
func (m Model) handlePressDone(msg pressDoneMsg) Model {
    if msg.err != nil || msg.result == nil || msg.result.Status != "ok" {
        m.status[msg.name] = statusFailed
        m.lastOK = ""
        m.lastErr = fmt.Sprintf("press %s: %s", msg.name, errTypeFrom(msg))
        return m
    }
    m.status[msg.name] = statusOK
    m.lastErr = ""
    m.lastOK = msg.name
    return m
}
```

Batteries (env injection from `~/.buttons/batteries`) are loaded *per
press*, not at startup, so a battery added in another shell shows up
without a TUI restart.

---

## Mechanical press choreography

Terminals don't have CSS `:active`. We fake it with a **four-frame
timeline** scheduled by `tea.Tick`. The model grows two transient
fields and two messages:

```go
// Model fields
pressPulse     string // name of row currently showing "depressed" frame
pressPulseFire bool   // true during the fire frame (indicator fill)

// Messages
type pressFireMsg    struct{ name string } // +40ms — swap to fire frame
type pressReleaseMsg struct{ name string } // +180ms — back to rest
```

Schedule three things on Enter/Space:

```go
case "enter", " ":
    name := m.currentButtonName()
    m.pressPulse = name
    return m, tea.Batch(
        tea.Tick(40*time.Millisecond,  func(_ time.Time) tea.Msg { return pressFireMsg{name} }),
        tea.Tick(180*time.Millisecond, func(_ time.Time) tea.Msg { return pressReleaseMsg{name} }),
        runPress(btn, codePath, batteries),
    )
```

Handle the pulse messages:

```go
case pressFireMsg:
    if m.pressPulse == msg.name { m.pressPulseFire = true }
    return m, nil

case pressReleaseMsg:
    if m.pressPulse == msg.name {
        m.pressPulse = ""
        m.pressPulseFire = false
    }
    return m, nil
```

Render uses the pulse state to pick the right style for just this
frame:

```go
func (m Model) renderPinnedCard(btn button.Button, selected bool) string {
    style := m.styles.PinnedIdle
    switch {
    case m.status[btn.Name] == statusRunning:
        style = m.styles.PinnedActive
    case m.pressPulse == btn.Name && m.pressPulseFire:
        style = m.styles.PinnedActive            // orange fire frame
    case m.pressPulse == btn.Name:
        style = m.styles.PinnedSelected          // depressed frame (thick border)
    case selected:
        style = m.styles.PinnedSelected
    }
    // ...
}
```

### The four frames

| Frame   | When         | Style                     | What the user sees                               |
|---------|--------------|---------------------------|--------------------------------------------------|
| Rest    | —            | `PinnedIdle`              | Normal border, muted foreground                  |
| Keydown | +0ms         | `PinnedSelected`          | **Thick** border, bold — "I received the press"  |
| Fire    | +40ms        | `PinnedActive`            | Thick **orange** border, orange fg — "committed" |
| Release | +180ms       | back to idle / active     | Either rest, or persistent active if still running |

The release can resolve *into* running-active, and it does so
seamlessly because both frames share the thick orange border. The user
sees "I pressed it → it's running" as one continuous gesture.

### Mouse parity

Bubble Tea v2 distinguishes `MouseClickMsg` (logical click) from
`MouseMotionMsg` (hover / drag). We treat `MouseClickMsg` exactly like
Enter — same `pressPulse` schedule, same fire/release timing. Terminal
mouse events don't give us real press/release semantics, so emulating
a timed pulse is the right move anyway.

### List row pulse

List rows get a subtler treatment: a 1-cell glyph swap plus a
`ButtonNameActive` reveal for the keydown frame. The three-glyph cycle:

```
  □ deploy    →    ▣ deploy    →    ◉ deploy    →    ✓ deploy
   rest         keydown +40ms     fire +80ms        done (or back to idle)
```

All four are rendered by `Styles.indicator()` with branch logic on
`pressPulse` / `pressPulseFire`. No new styles needed.

---

## The logs view

Toggled by `L`, sits between the list and the footer. Keep it
**minimal** — it's a peek, not a dashboard. The full-screen logs view
described below is reached by pressing `↵` on an active row or
`buttons logs <name>` from the CLI.

### Inline pane (5-row peek)

```go
const logsPaneLimit = 5

func (m Model) renderLogs() string {
    title := m.styles.HeroTitle.Render("logs")
    target := m.currentButtonName()
    if target == "" {
        return indentBlock(title+"\n\n"+m.styles.Muted.Render("focus a button to see its history"), leftPad)
    }

    runs, err := history.List(target, logsPaneLimit)
    if err != nil || len(runs) == 0 {
        empty := m.styles.Muted.Render(
            fmt.Sprintf("no runs for %s yet — press it to record one.", target))
        return indentBlock(title+m.styles.Muted.Render("  ·  "+target)+"\n\n"+empty, leftPad)
    }

    // Width budget: subtract indent + fixed columns (glyph+time+exit+dur = ~38)
    previewBudget := m.width - leftPad - 2 - 38
    if previewBudget < 20 { previewBudget = 20 }

    lines := []string{
        title + m.styles.Muted.Render(fmt.Sprintf("  ·  %s  ·  last %d", target, len(runs))),
        "",
    }
    for _, r := range runs {
        lines = append(lines, m.renderLogRow(r, previewBudget))
    }
    return indentBlock(strings.Join(lines, "\n"), leftPad)
}
```

Each row: `glyph · time · exit · duration · preview`. Preview is the
first non-empty line of stdout (stderr when failed), truncated to fit:

```go
func (m Model) renderLogRow(r history.Run, previewBudget int) string {
    glyph := m.styles.StatusOK.Render("✓")
    if r.Status != "ok" { glyph = m.styles.StatusError.Render("✗") }

    localTime := r.StartedAt.Local().Format("15:04:05")
    meta := fmt.Sprintf("exit %-3d  %5dms", r.ExitCode, r.DurationMs)

    source := r.Stdout
    if r.Status != "ok" && r.Stderr != "" { source = r.Stderr }
    preview := truncateDisplay(firstLineTrimmed(source), previewBudget)

    return fmt.Sprintf("  %s  %s  %s  %s",
        glyph,
        m.styles.Muted.Render(localTime),
        m.styles.Muted.Render(meta),
        m.styles.Secondary.Render(preview))
}
```

### Full-screen logs view (the `buttons logs <name>` subcommand)

A second Bubble Tea program, invoked either from the CLI or by pressing
`↵` on an active row in the board. Scope: **one press, live stream**.

Minimal model:

```go
// internal/tui/logs.go
type LogsModel struct {
    styles  Styles
    name    string         // button being followed
    press   string         // press id (e.g. "0c5b")
    pid     int
    started time.Time
    lines   []logLine      // streamed
    follow  bool           // true = pin to tail
    cancel  context.CancelFunc
    done    bool
    exit    int
}

type logLine struct {
    ts    time.Time
    sev   severity  // info, stdout, warn, stderr
    text  string
}

type logLineMsg logLine
type logDoneMsg struct{ exit int }
```

Input handling:

```go
case "f":       m.follow = !m.follow
case "g":       /* jump to top */
case "G":       /* jump to bottom, re-enable follow */
case "esc", "q": return m, tea.Quit
case "ctrl+c":  if m.cancel != nil { m.cancel() }
```

**Streaming is a goroutine that sends `logLineMsg`** to the program.
`engine.Execute` exposes a line channel; bridge it to Bubble Tea:

```go
func streamLogs(ch <-chan engine.LogLine) tea.Cmd {
    return func() tea.Msg {
        line, ok := <-ch
        if !ok { return logDoneMsg{} }
        return logLineMsg(toLogLine(line))
    }
}

// In Update, re-arm after each message:
case logLineMsg:
    m.lines = append(m.lines, logLine(msg))
    return m, streamLogs(ch)
```

Render is three blocks:

1. **Header**: `deploy — shell · env=staging  ·  started 12:31:58  ·  press 0c5b  ·  pid 41238` +
   right-aligned elapsed counter and spinner.
2. **Stream**: each line rendered as `ts sev text`, with severity
   colored via styles (`Indicator` for error/stderr, `Secondary` for
   stdout, `Muted` for info, a warn-yellow we'd add).
3. **Footer**: `■ cancel`  +  `f follow · g/G top/end · esc back`.

**Strip for simplicity**: no tabs, no sparkline, no CPU/mem/IO meters.
The stream is the product. If someone wants statistics, they pipe the
log file out of `~/.buttons/buttons/<name>/pressed/`.

### A new severity-warn style

Add to `styles.go` — the one new style we need:

```go
// In Styles struct:
StatusWarn lipgloss.Style

// In BuildStyles:
colorWarn := lipgloss.Color("#F0C060") // amber, for `warn` severity only
// ...
StatusWarn: lipgloss.NewStyle().Foreground(colorWarn),
```

Use only for `severity == warn` log lines. Not decorative.

---

## Themes (paper · phosphor · amber)

Gate on `BUTTONS_THEME`:

```go
type ThemeName string
const (
    ThemeDefault  ThemeName = "default"
    ThemePaper    ThemeName = "paper"
    ThemePhosphor ThemeName = "phosphor"
    ThemeAmber    ThemeName = "amber"
)

func themeFromEnv() ThemeName {
    switch os.Getenv("BUTTONS_THEME") {
    case "paper":    return ThemePaper
    case "phosphor": return ThemePhosphor
    case "amber":    return ThemeAmber
    default:         return ThemeDefault
    }
}
```

Extend `BuildStyles` to branch on theme *only for palette*, not for
style structure. Every theme must define all the same styles — only
the colors differ:

```go
func BuildStyles() Styles {
    theme := themeFromEnv()

    var primary, secondary, muted, indicator lipgloss.Color
    switch theme {
    case ThemePhosphor:
        primary   = lipgloss.Color("#b8f3c3") // phosphor green
        secondary = lipgloss.Color("#6c9e78")
        muted     = lipgloss.Color("#2a4a32")
        indicator = lipgloss.Color("#ff8a6c") // warm red for ACTIVE
    case ThemeAmber:
        primary   = lipgloss.Color("#f7c472")
        secondary = lipgloss.Color("#a17a3c")
        muted     = lipgloss.Color("#3a2a15")
        indicator = lipgloss.Color("#ffbe5a")
    case ThemePaper:
        primary   = lipgloss.Color(hexInk)
        secondary = lipgloss.Color(hexDust)
        muted     = lipgloss.Color(hexAluminum)
        indicator = lipgloss.Color(hexIndicator)
    default:
        hasDark := lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
        ld := lipgloss.LightDark(hasDark)
        primary   = ld(lipgloss.Color(hexInk), lipgloss.Color(hexPaper)).(lipgloss.Color)
        secondary = ld(lipgloss.Color(hexDust), lipgloss.Color("#A8A8A0")).(lipgloss.Color)
        muted     = ld(lipgloss.Color(hexAluminum), lipgloss.Color("#3A3A38")).(lipgloss.Color)
        indicator = lipgloss.Color(hexIndicator)
    }

    // ... build Styles struct using these four colors
}
```

**Rule:** Phosphor and amber use warmer indicators because orange
against phosphor-green vibrates painfully. The indicator's *role*
(ACTIVE only) is preserved — its *hex* changes per theme.

---

## Testing the TUI

### Unit tests — pure functions first

`Update` is pure, so you can drive it directly:

```go
func TestPressButtonFlipsStatus(t *testing.T) {
    svc := newFakeService(t, button.Button{Name: "weather", Runtime: "http"})
    m, _ := New(svc, "weather")

    next, _ := m.Update(tea.KeyPressMsg{/* enter */})
    nm := next.(Model)

    if nm.status["weather"] != statusRunning {
        t.Fatalf("want running, got %v", nm.status["weather"])
    }
}
```

### Snapshot tests — `View()` is a string

```go
func TestBoardIdleSnapshot(t *testing.T) {
    m := newFixtureModel(t)
    m.width, m.height = 80, 24
    v := m.View()
    snaps.MatchSnapshot(t, v.String())
}
```

Strip ANSI with `ansi.Strip` if you want to diff plain text:

```go
import "charm.land/x/ansi"
plain := ansi.Strip(v.String())
```

### Integration — `teatest` from `bubbletea/testing`

```go
tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
tm.Send(tea.KeyPressMsg{/* enter */})
teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
    return bytes.Contains(b, []byte("ACTIVE"))
})
```

### What to test

- Cursor wrap (up at index 0 → last item).
- Tab section-switch with and without a pinned row.
- Press blocked while another press is running.
- Required-arg bail renders a `StatusError` line.
- `refreshMsg` trims `cursorList` when the list shrinks.
- Click at a given `(x, y)` hits the right region per `computeLayout`.

---

## Anti-patterns we've rejected

- **No tab bar on the logs view.** Five tabs (stream/stdout/stderr/env/history) is a dashboard, not a logs tail. If users need stderr-only, `grep` is right there.
- **No progress bars in the board.** Spinners communicate "working", not "34% done". We don't know the percentage.
- **No quit key in the footer legend.** The board is ambient. `ctrl+c` stays unadvertised as an escape hatch.
- **No ticking when idle.** `tickCmd()` is re-armed only when `anyRunning()`. An open idle board should be 0% CPU.
- **No mutating the model pointer.** `Update` returns a value-type copy. Mutating and returning the original silently drops updates.
- **No indicator orange anywhere but ACTIVE.** Tempting on dividers, headers, empty-state accents. Every time: don't.
- **No pixel-perfect CSS framework envy.** Four-frame `tea.Tick`-scheduled style swaps are plenty. Stop before you write an animation engine.

---

## Quick reference: adding a new screen

1. Add a new state to a `section` enum (or a new top-level `Model.view` field).
2. Write `renderX()` returning a string, composed of Styles-rendered pieces.
3. Add its Y range to `layout` and update `computeLayout`.
4. Add key handlers in `handleKey` — bind to a capital letter if a lowercase collides with a vim motion.
5. Extend `handleLeftClick` for any clickable regions.
6. Add a snapshot test for the new render.
7. If it animates, use `tea.Tick` — never spawn goroutines that touch the model.

The TUI fits in ~1000 lines of Go. Keep it that way.
