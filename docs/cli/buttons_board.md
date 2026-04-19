---
title: "buttons board"
description: "CLI reference for buttons board"
---

## buttons board

Open the button board in a new terminal window

### Synopsis

The board is the human UI for buttons — an always-on dashboard
that shows every button in the active project, their state, and
recent press outcomes. Agents never invoke it; they use the CLI.

By default, running `buttons board` opens the dashboard in a new
terminal window on the host OS and returns to your current shell. The
new window runs until you close it.

Use `--inline` to render the board in the current terminal instead —
helpful when you're SSH'd somewhere, inside a screen / tmux session
you want to stay in, or on a headless machine without a window server.

```
buttons board [name] [flags]
```

### Options

```
  -h, --help     help for board
      --inline   render the board in the current terminal instead of spawning a new window
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

