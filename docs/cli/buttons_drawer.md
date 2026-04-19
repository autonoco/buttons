---
title: "buttons drawer"
description: "CLI reference for buttons drawer"
---

## buttons drawer

Manage drawer workflows (chains of buttons)

### Synopsis

Manage drawers — typed workflows that chain buttons with
${ref} references between steps.

Usage:
  buttons drawer create NAME
  buttons drawer list
  buttons drawer NAME add BUTTON [BUTTON...]
  buttons drawer NAME connect A to B
  buttons drawer NAME connect A.output.x to B.args.y
  buttons drawer NAME press [key=value ...]
  buttons drawer NAME remove
  buttons drawer NAME                  (show drawer summary)
  buttons drawer schema                (print JSON Schema)

```
buttons drawer [flags]
```

### Options

```
      --failed NAME logs   only return runs that failed (for NAME logs)
  -h, --help               help for drawer
      --limit NAME logs    max runs to return (for NAME logs) (default 20)
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

