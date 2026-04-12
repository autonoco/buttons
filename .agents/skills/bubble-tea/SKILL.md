---
name: bubble-tea
description: >
    Build terminal UIs in Go with Bubble Tea v2 (Elm Architecture). Covers Model/Update/View lifecycle,
    commands (Batch, Sequence, Tick), key/mouse handling, Program options, and Charm ecosystem (Bubbles, Lip Gloss, Huh).

    Use when: building Go TUI apps, creating terminal interfaces, handling keyboard/mouse input in TUI,
    using Bubble Tea or Charm libraries, or troubleshooting "tea.Model" or "tea.Cmd" issues.
---

Apply these patterns when building terminal UIs with Bubble Tea v2. For full API reference, see [references/](references/).

**Important**: This covers Bubble Tea **v2** (`github.com/charmbracelet/bubbletea/v2`). Key v2 changes from v1:
- `View()` returns `tea.View` (not `string`) — use `tea.NewView(s)`
- Key events are `tea.KeyPressMsg` (not `tea.KeyMsg`)
- `tea.Quit` is now `tea.Quit()` (function call, returns `Msg`)
- `Init()` returns `tea.Cmd` (not `(tea.Model, tea.Cmd)` in basic form)

## Core Architecture (Elm Architecture)

Every Bubble Tea program has three parts:

```go
type Model interface {
    Init() tea.Cmd             // initial command (or nil)
    Update(tea.Msg) (tea.Model, tea.Cmd)  // handle events, return new state + command
    View() tea.View            // render UI from state
}
```

1. **Init** — runs once at startup, returns an initial command (or `nil`)
2. **Update** — receives messages (events), returns updated model + next command
3. **View** — pure function: model in, string out (wrapped in `tea.NewView`)

## Minimal Example

```go
package main

import (
    "fmt"
    "os"
    tea "github.com/charmbracelet/bubbletea/v2"
)

type model struct {
    cursor   int
    choices  []string
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
    s := "What should we buy?\n\n"
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

## Commands (Cmd)

Commands perform I/O and return messages. **Never do I/O in Update or View** — return a `Cmd` instead.

```go
// nil = no command
return m, nil

// Single command
return m, fetchData

// Run commands concurrently (no ordering guarantee)
return m, tea.Batch(cmd1, cmd2, cmd3)

// Run commands sequentially (one at a time, in order)
return m, tea.Sequence(cmd1, cmd2, cmd3)
```

### Custom Command Pattern

```go
type DataMsg struct{ Items []Item }
type ErrMsg struct{ Err error }

func fetchData() tea.Msg {
    items, err := api.GetItems()
    if err != nil {
        return ErrMsg{err}
    }
    return DataMsg{items}
}

// In Update:
case tea.KeyPressMsg:
    if msg.String() == "r" {
        return m, fetchData  // pass function as Cmd
    }
case DataMsg:
    m.items = msg.Items
case ErrMsg:
    m.err = msg.Err
```

### Tick / Timer

```go
type TickMsg time.Time

func doTick() tea.Cmd {
    return tea.Tick(time.Second, func(t time.Time) tea.Msg {
        return TickMsg(t)
    })
}

func (m model) Init() tea.Cmd { return doTick() }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg.(type) {
    case TickMsg:
        m.elapsed++
        return m, doTick()  // loop: return another tick
    }
    return m, nil
}
```

`tea.Every` is similar but syncs to system clock (fires at exact intervals).

### Conditional Batching

```go
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    var cmds []tea.Cmd
    if m.needsRefresh {
        cmds = append(cmds, refreshCmd)
    }
    cmds = append(cmds, saveCmd)
    return m, tea.Batch(cmds...)
}
```

## Key Handling (v2)

```go
case tea.KeyPressMsg:
    // By string representation
    switch msg.String() {
    case "q", "ctrl+c":
        return m, tea.Quit
    case "enter":
        // handle enter
    case "up", "k":
        // handle up
    }

    // By key code
    switch msg.Key().Code {
    case tea.KeyEnter:
    case tea.KeyTab:
    case tea.KeyEscape:
    }

    // By keystroke (includes modifiers)
    switch msg.Keystroke() {
    case "ctrl+c":
    case "shift+alt+a":
    }

    // Printable characters
    if msg.Key().Text != "" {
        m.input += msg.Key().Text
    }
```

### Common Key Constants

```go
tea.KeyUp, tea.KeyDown, tea.KeyLeft, tea.KeyRight
tea.KeyEnter, tea.KeyReturn, tea.KeyTab, tea.KeyEscape
tea.KeyBackspace, tea.KeyDelete, tea.KeySpace
tea.KeyHome, tea.KeyEnd, tea.KeyPgUp, tea.KeyPgDown
tea.KeyF1 ... tea.KeyF63
```

### Modifier Keys

```go
tea.ModCtrl, tea.ModAlt, tea.ModShift
tea.ModMeta, tea.ModHyper, tea.ModSuper
```

## Mouse Handling

```go
case tea.MouseClickMsg:
    x, y := msg.Mouse().X, msg.Mouse().Y
    button := msg.Mouse().Button  // tea.MouseLeft, tea.MouseRight, etc.

case tea.MouseWheelMsg:
    // scroll events

case tea.MouseMotionMsg:
    // hover/drag events
```

Mouse buttons: `MouseLeft`, `MouseMiddle`, `MouseRight`, `MouseWheelUp`, `MouseWheelDown`, `MouseBackward`, `MouseForward`.

## Window Size

```go
case tea.WindowSizeMsg:
    m.width = msg.Width
    m.height = msg.Height
```

Request explicitly: `tea.RequestWindowSize()`

## Built-in Messages

```go
tea.Quit()                    // quit the program
tea.ClearScreen()             // clear terminal
tea.Suspend()                 // suspend (like Ctrl+Z)
tea.SetClipboard("text")      // set clipboard
tea.ReadClipboard()           // read clipboard
tea.RequestWindowSize()       // request window dimensions
tea.RequestCursorPosition()   // request cursor position
```

## Program Setup

```go
p := tea.NewProgram(
    initialModel(),
    // Options:
    tea.WithOutput(os.Stderr),          // custom output writer
    tea.WithInput(customReader),        // custom input reader
    tea.WithFPS(60),                    // framerate (default: 60)
    tea.WithContext(ctx),               // context for cancellation
    tea.WithWindowSize(80, 24),         // initial window size
    tea.WithoutSignalHandler(),         // disable signal handling
    tea.WithoutCatchPanics(),           // let panics propagate
    tea.WithFilter(filterFunc),         // message filter
)

model, err := p.Run()  // blocking
```

### Sending Messages from Outside

```go
p := tea.NewProgram(m)
go func() {
    p.Send(MyCustomMsg{data: "hello"})
}()
p.Run()
```

### Printing Outside the TUI

```go
// From within Update (as Cmd):
return m, tea.Println("Status: done")
return m, tea.Printf("Processed %d items", count)

// From outside:
p.Println("External message")
```

## Focus and Blur

```go
case tea.FocusMsg:
    m.focused = true
case tea.BlurMsg:
    m.focused = false
```

## Debugging

```go
// Log to file (since TUI owns stdout)
if os.Getenv("DEBUG") != "" {
    f, err := tea.LogToFile("debug.log", "debug")
    if err != nil {
        fmt.Println("fatal:", err)
        os.Exit(1)
    }
    defer f.Close()
}
// Then use log.Println() throughout your code
// Monitor: tail -f debug.log
```

## Charm Ecosystem

| Library | Purpose | Import |
|---------|---------|--------|
| **Bubbles** | Reusable components (text input, viewport, spinner, list, table, paginator, progress bar) | `github.com/charmbracelet/bubbles` |
| **Lip Gloss** | Styling and layout (colors, borders, padding, alignment) | `github.com/charmbracelet/lipgloss` |
| **Huh** | Interactive forms and prompts | `github.com/charmbracelet/huh` |
| **Harmonica** | Spring animations | `github.com/charmbracelet/harmonica` |
| **BubbleZone** | Mouse event zone tracking | `github.com/lrstanley/bubblezone` |

### Bubbles Components Pattern

```go
import "github.com/charmbracelet/bubbles/textinput"

type model struct {
    textInput textinput.Model
}

func initialModel() model {
    ti := textinput.New()
    ti.Placeholder = "Search..."
    ti.Focus()
    return model{textInput: ti}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    var cmd tea.Cmd
    m.textInput, cmd = m.textInput.Update(msg)  // delegate to component
    return m, cmd
}

func (m model) View() tea.View {
    return tea.NewView(m.textInput.View())
}
```

## Common Patterns

### Multiple Components

```go
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    var cmds []tea.Cmd

    // Update each component, collect commands
    var cmd tea.Cmd
    m.input, cmd = m.input.Update(msg)
    cmds = append(cmds, cmd)

    m.list, cmd = m.list.Update(msg)
    cmds = append(cmds, cmd)

    return m, tea.Batch(cmds...)
}
```

### State Machine (Screens/Views)

```go
type state int
const (
    stateMenu state = iota
    stateInput
    stateConfirm
)

type model struct {
    state state
    // ...
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch m.state {
    case stateMenu:
        return m.updateMenu(msg)
    case stateInput:
        return m.updateInput(msg)
    case stateConfirm:
        return m.updateConfirm(msg)
    }
    return m, nil
}

func (m model) View() tea.View {
    switch m.state {
    case stateMenu:
        return tea.NewView(m.menuView())
    case stateInput:
        return tea.NewView(m.inputView())
    case stateConfirm:
        return tea.NewView(m.confirmView())
    }
    return tea.NewView("")
}
```

### Running External Processes

```go
cmd := exec.Command("vim", "file.txt")
return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
    return editorFinishedMsg{err}
})
```

This pauses the TUI, runs the process with full terminal access, then resumes.

## Common Mistakes

- **Doing I/O in Update**: Never. Return a `Cmd` instead.
- **Forgetting to return a Cmd from Init**: Return `nil` if no initial I/O needed.
- **Not delegating Update to sub-components**: Bubbles components need their `Update` called.
- **Using `string` return from View (v1 style)**: v2 requires `tea.View` — use `tea.NewView(s)`.
- **Using `tea.KeyMsg` (v1)**: v2 uses `tea.KeyPressMsg` and `tea.KeyReleaseMsg`.
- **Using `tea.Quit` as a value (v1)**: v2 requires `tea.Quit()` (function call).
- **Printing to stdout**: TUI owns stdout. Use `tea.LogToFile` for debugging.
- **Not collecting Cmds from sub-components**: Use `tea.Batch(cmds...)` to combine.
