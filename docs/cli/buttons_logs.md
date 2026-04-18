---
title: "buttons logs"
description: "CLI reference for buttons logs"
---

## buttons logs

Press a button and watch its output stream live

### Synopsis

Press a button in a full-screen viewer that tails every line of
stdout / stderr as the child writes it.

The viewer stays open after the press completes so you can scroll
the output at leisure. Press esc or q to dismiss; ctrl+c cancels an
in-flight press (the child's process group is killed).

Scope is one press. If the button takes required args, pass them with
--arg key=value the same way 'buttons press' does.

Only shell and code buttons stream today. HTTP and prompt buttons
still use 'buttons press' for now — their execution is request /
response, not a long-running process.

**Examples:**

```bash
buttons logs deploy
buttons logs deploy --arg env=staging
buttons logs etl --arg file=/tmp/x.csv
```

```
buttons logs [name] [flags]
```

### Options

```
      --arg stringArray   argument as key=value (repeatable; validated against the button spec)
  -h, --help              help for logs
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

