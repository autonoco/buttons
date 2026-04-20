---
title: "buttons logs"
description: "CLI reference for buttons logs"
---

## buttons logs

View past runs for a button or workspace failures

### Synopsis

Structured run history. CLI only — no TUI. For a live-stream
viewer of an in-flight press, use the board: `buttons board`.

  buttons BUTTONNAME logs            — past runs for this button
  buttons BUTTONNAME logs --failed   — just failures
  buttons BUTTONNAME logs --limit 10 — how many (default 20)
  buttons drawer DRAWERNAME logs     — past runs for this drawer
  buttons logs                       — recent failures across the workspace

Agent mode (--json or non-TTY) returns the full Run shape (status,
exit_code, duration_ms, stdout, stderr, error_type, args). TTY mode
prints a compact one-line-per-run table. The verb-first form
(buttons logs NAME) still works as an alias.

```
buttons logs [name] [flags]
```

### Options

```
      --failed      only return runs that failed
  -h, --help        help for logs
      --limit int   max runs to return (default 20)
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

