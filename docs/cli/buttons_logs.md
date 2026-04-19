---
title: "buttons logs"
description: "CLI reference for buttons logs"
---

## buttons logs

View a button's past runs, or press and stream live

### Synopsis

View a button's run history. Preferred form is name-first to
match the rest of the CLI:

  buttons BUTTONNAME logs           — past runs for this button
  buttons BUTTONNAME logs --follow  — press + stream live
  buttons BUTTONNAME logs --failed  — just failures
  buttons drawer DRAWERNAME logs    — past runs for this drawer

The verb-first form (buttons logs NAME) still works as an alias.
buttons logs (no name) dumps recent failures across every button
and drawer — same shape as summary.recent_failures.

```
buttons logs [name] [flags]
```

### Options

```
      --arg stringArray   argument as key=value (with --follow, passed through to the press)
      --failed            only return runs that failed
  -f, --follow            press the button and stream live output in a TUI
  -h, --help              help for logs
      --limit int         max runs to return (default 20)
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

