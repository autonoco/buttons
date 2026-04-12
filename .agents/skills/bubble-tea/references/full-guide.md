# Bubble Tea v2 — Full API Reference

Source: [pkg.go.dev/github.com/charmbracelet/bubbletea/v2](https://pkg.go.dev/github.com/charmbracelet/bubbletea/v2)

Package `tea` — A Go framework for building rich terminal user interfaces based on The Elm Architecture.

**Version**: v2 (beta, production-ready)
**License**: MIT
**Import**: `tea "github.com/charmbracelet/bubbletea/v2"`

---

## Core Interface: Model

```go
type Model interface {
    Init() Cmd
    Update(Msg) (Model, Cmd)
    View() View
}
```

### The Elm Architecture Lifecycle

1. **Init()** — Perform initial setup, return optional initial command
2. **Update(Msg)** — React to messages, update state, return new model + command
3. **View()** — Render current state as UI (pure function, no side effects)

---

## Type: Program

```go
type Program struct { }

func NewProgram(model Model, opts ...ProgramOption) *Program

func (p *Program) Run() (returnModel Model, returnErr error)
func (p *Program) Send(msg Msg)
func (p *Program) Quit()
func (p *Program) Kill()
func (p *Program) Wait()

func (p *Program) Printf(template string, args ...any)
func (p *Program) Println(args ...any)

func (p *Program) ReleaseTerminal() error
func (p *Program) RestoreTerminal() error
```

### Program Options

```go
// Window and rendering
WithWindowSize(width, height int) ProgramOption
WithOutput(output io.Writer) ProgramOption
WithInput(input io.Reader) ProgramOption
WithFPS(fps int) ProgramOption

// Color and environment
WithColorProfile(profile colorprofile.Profile) ProgramOption
WithEnvironment(env []string) ProgramOption

// Behavior
WithContext(ctx context.Context) ProgramOption
WithFilter(filter func(Model, Msg) Msg) ProgramOption

// Signal handling
WithoutSignalHandler() ProgramOption
WithoutSignals() ProgramOption
WithoutCatchPanics() ProgramOption

// Rendering control
WithoutRenderer() ProgramOption
```

---

## Type: Cmd

Commands perform I/O and return messages.

```go
type Cmd func() Msg

func Batch(cmds ...Cmd) Cmd         // run concurrently, no ordering
func Sequence(cmds ...Cmd) Cmd      // run one at a time, in order
func Tick(d time.Duration, fn func(time.Time) Msg) Cmd  // timer
func Every(duration time.Duration, fn func(time.Time) Msg) Cmd  // system clock sync
func ExecProcess(c *exec.Cmd, fn ExecCallback) Cmd  // run external process
func Exec(c ExecCommand, fn ExecCallback) Cmd

// Print outside the managed UI
func Println(args ...any) Cmd
func Printf(template string, args ...any) Cmd

// Clipboard
func SetClipboard(s string) Cmd
func SetPrimaryClipboard(s string) Cmd

// Terminal capabilities
func RequestCapability(s string) Cmd
func Raw(r any) Cmd
```

---

## Type: Msg

Messages represent events. All built-in messages:

```go
type Msg interface{}

// Quit/control
func Quit() Msg
func Interrupt() Msg
func ClearScreen() Msg
func Suspend() Msg

// Terminal state requests
func RequestWindowSize() Msg
func RequestCursorPosition() Msg
func RequestTerminalVersion() Msg
func RequestForegroundColor() Msg
func RequestBackgroundColor() Msg
func RequestCursorColor() Msg

// Clipboard
func ReadClipboard() Msg
func ReadPrimaryClipboard() Msg
```

### Built-in Message Types

```go
// Keyboard
type KeyPressMsg Key       // Key press event (v2 — replaces KeyMsg from v1)
type KeyReleaseMsg Key     // Key release event

// Window
type WindowSizeMsg struct { Width, Height int }

// Mouse
type MouseClickMsg struct { }
type MouseMotionMsg struct { }
type MouseReleaseMsg struct { }
type MouseWheelMsg struct { }

// Focus
type FocusMsg struct{}
type BlurMsg struct{}

// Paste
type PasteStartMsg struct{}
type PasteMsg string
type PasteEndMsg struct{}

// Lifecycle
type InterruptMsg struct{}
type SuspendMsg struct{}
type ResumeMsg struct{}
type QuitMsg struct{}

// Terminal info
type ColorProfileMsg struct { colorprofile.Profile }
type BackgroundColorMsg struct { color.Color }
type ForegroundColorMsg struct { color.Color }
type CursorColorMsg struct { color.Color }
type CursorPositionMsg Position
type TerminalVersionMsg string
type CapabilityMsg string
type KeyboardEnhancementsMsg uint8

// Environment
type EnvMsg uv.Environ
func (msg EnvMsg) Getenv(key string) string
func (msg EnvMsg) LookupEnv(key string) (string, bool)

// Clipboard
type ClipboardMsg struct {
    Content   string
    Selection byte  // 'c' = system, 'p' = primary
}

// Internal
type BatchMsg []Cmd
type RawMsg any
```

---

## Type: Key

```go
type Key struct {
    Text        string      // Printable characters
    Code        rune        // The key code
    ShiftedCode rune        // Shifted version (Kitty/Windows)
    BaseCode    rune        // PC-101 layout code (Kitty/Windows)
    Mod         KeyMod      // Modifier keys
    IsRepeat    bool        // Key being held down
}

func (k Key) String() string      // Text representation
func (k Key) Keystroke() string   // Full keystroke like "ctrl+a"

type KeyMsg interface {
    Key() Key
    fmt.Stringer
}

type KeyPressMsg Key
type KeyReleaseMsg Key
```

### Special Key Constants

```go
// Arrow keys
KeyUp, KeyDown, KeyLeft, KeyRight

// Navigation
KeyHome, KeyEnd, KeyPgUp, KeyPgDown
KeyInsert, KeyDelete

// Function keys
KeyF1 ... KeyF63

// Special
KeyEnter, KeyReturn, KeyTab
KeyEscape, KeyEsc
KeyBackspace, KeySpace

// Modifiers
ModCtrl, ModAlt, ModShift
ModMeta, ModHyper, ModSuper
ModCapsLock, ModNumLock, ModScrollLock
```

### Key Matching Patterns

```go
// By string
switch msg.String() {
case "enter": ...
case "a": ...
case "ctrl+c": ...
}

// By key code
switch msg.Key().Code {
case KeyEnter: ...
case KeyTab: ...
}

// By keystroke (with modifiers)
switch msg.Keystroke() {
case "ctrl+c": ...
case "shift+alt+a": ...
}

// Printable characters
if msg.Key().Text != "" {
    // handle typed text
}
```

---

## Type: Mouse

```go
type Mouse struct {
    X, Y   int
    Button MouseButton
    Mod    KeyMod
}

const (
    MouseNone MouseButton = iota
    MouseLeft
    MouseMiddle
    MouseRight
    MouseWheelUp
    MouseWheelDown
    MouseWheelLeft
    MouseWheelRight
    MouseBackward
    MouseForward
    MouseButton10
    MouseButton11
)
```

---

## Type: View and Rendering

```go
type View struct{}
func NewView(s any) View
func (v *View) SetContent(s any)

type Layer interface {
    Draw(s Screen, r Rectangle)
}

type Cursor struct {
    Position Position
    Color    color.Color
    Shape    CursorShape
    Blink    bool
}

func NewCursor(x, y int) *Cursor

type CursorShape int
const (
    CursorBlock CursorShape = iota
    CursorUnderline
    CursorBar
)

type Position struct { X, Y int }

type Hittable interface {
    Hit(x, y int) string
}

type LayerHitMsg struct {
    ID    string
    Mouse MouseMsg
}
```

---

## Logging

```go
func LogToFile(path string, prefix string) (*os.File, error)
func LogToFileWith(path string, prefix string, log LogOptionsSetter) (*os.File, error)

type LogOptionsSetter interface {
    SetOutput(io.Writer)
    SetPrefix(string)
}
```

Usage:
```go
if os.Getenv("DEBUG") != "" {
    f, err := tea.LogToFile("debug.log", "debug")
    if err != nil {
        fmt.Println("fatal:", err)
        os.Exit(1)
    }
    defer f.Close()
}
// Then use log.Println() throughout
// Monitor: tail -f debug.log
```

---

## Error Variables

```go
var ErrProgramPanic = errors.New("program experienced a panic")
var ErrProgramKilled = errors.New("program was killed")
var ErrInterrupted = errors.New("program was interrupted")
```

---

## TTY Operations

```go
func OpenTTY() (*os.File, *os.File, error)
```

---

## Complete Example: Shopping List

```go
package main

import (
    "fmt"
    "os"
    tea "github.com/charmbracelet/bubbletea/v2"
)

type model struct {
    choices  []string
    cursor   int
    selected map[int]struct{}
}

func initialModel() model {
    return model{
        choices:  []string{"Buy carrots", "Buy celery", "Buy kohlrabi"},
        selected: make(map[int]struct{}),
    }
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyPressMsg:
        switch msg.String() {
        case "ctrl+c", "q":
            return m, tea.Quit
        case "up", "k":
            if m.cursor > 0 { m.cursor-- }
        case "down", "j":
            if m.cursor < len(m.choices)-1 { m.cursor++ }
        case "enter", " ":
            if _, ok := m.selected[m.cursor]; ok {
                delete(m.selected, m.cursor)
            } else {
                m.selected[m.cursor] = struct{}{}
            }
        }
    }
    return m, nil
}

func (m model) View() tea.View {
    s := "What should we buy at the market?\n\n"
    for i, choice := range m.choices {
        cursor, checked := " ", " "
        if m.cursor == i { cursor = ">" }
        if _, ok := m.selected[i]; ok { checked = "x" }
        s += fmt.Sprintf("%s [%s] %s\n", cursor, checked, choice)
    }
    s += "\nPress q to quit.\n"
    return tea.NewView(s)
}

func main() {
    if _, err := tea.NewProgram(initialModel()).Run(); err != nil {
        fmt.Fprintf(os.Stderr, "error: %v\n", err)
        os.Exit(1)
    }
}
```

---

## v2 Breaking Changes from v1

| v1 | v2 |
|----|-----|
| `View() string` | `View() tea.View` (use `tea.NewView(s)`) |
| `tea.KeyMsg` | `tea.KeyPressMsg` / `tea.KeyReleaseMsg` |
| `tea.Quit` (value) | `tea.Quit()` (function call) |
| `tea.WithAltScreen()` | Removed — alt screen is now default |
| `tea.WithMouseCellMotion()` | Mouse enabled by default |

---

## Charm Ecosystem

| Library | Purpose | Import |
|---------|---------|--------|
| **Bubbles** | Reusable components (text input, viewport, spinner, list, table, paginator, progress) | `github.com/charmbracelet/bubbles` |
| **Lip Gloss** | Styling (colors, borders, padding, alignment, layout) | `github.com/charmbracelet/lipgloss` |
| **Huh** | Interactive forms and prompts | `github.com/charmbracelet/huh` |
| **Harmonica** | Spring animations | `github.com/charmbracelet/harmonica` |
| **BubbleZone** | Mouse event zone tracking | `github.com/lrstanley/bubblezone` |

---

## Notable Users

- **Microsoft** (Aztify), **CockroachDB**, **Truffle Security** (Trufflehog), **NVIDIA** (container-canary), **AWS** (eks-node-viewer), **MinIO** (mc), **Ubuntu** (Authd)
- **Charm projects**: Glow (markdown), Huh (forms), Mods (AI CLI), Wishlist (SSH)
- **Community**: chezmoi, circumflex, gh-dash, Tetrigo, Superfile

---

## Debugging Tips

1. **Don't print to stdout** — TUI owns it. Use `tea.LogToFile("debug.log", "debug")`.
2. **Headless Delve**: `dlv debug --headless --api-version=2 --listen=127.0.0.1:43000 .` then `dlv connect` from another terminal.
3. **Monitor logs**: `tail -f debug.log` in a separate terminal.
