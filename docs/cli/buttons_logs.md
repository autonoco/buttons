---
title: "buttons logs"
description: "CLI reference for buttons logs"
---

## buttons logs

View past runs for a button or tail the live progress stream

### Synopsis

Structured run history. CLI only — for the full-screen
interactive viewer, use 'buttons board'.

  buttons BUTTONNAME logs            — past runs for this button
  buttons BUTTONNAME logs --follow   — tail live progress JSONL as it writes
  buttons BUTTONNAME logs --failed   — just failures
  buttons BUTTONNAME logs --limit 10 — how many (default 20)
  buttons drawer DRAWERNAME logs     — past runs for this drawer
  buttons logs                       — recent failures across the workspace

--follow streams the latest press's progress events to stdout as
plain text (JSONL: one event per line). No TUI. Pipe it, parse it,
interrupt it with ctrl+C. Use this when an agent needs to watch a
long-running press live.

Agent mode (--json or non-TTY) returns the full Run shape for
non-follow calls. TTY mode prints a compact one-line-per-run table.
The verb-first form (buttons logs NAME) still works as an alias.

```
buttons logs [name] [flags]
```

### Options

```
      --failed      only return runs that failed
  -f, --follow      stream the latest press's progress events live (agent-friendly, no TUI)
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

