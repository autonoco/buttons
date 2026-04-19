---
title: "buttons list"
description: "CLI reference for buttons list"
---

## buttons list

List all buttons

### Synopsis

List all buttons in the registry.

Displays a table of buttons with name, runtime, file path, and timeout.
In non-TTY or --json mode, outputs the full button specs as JSON.

**Examples:**

```bash
buttons list
buttons list --json
```

```
buttons list [flags]
```

### Options

```
  -h, --help   help for list
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

