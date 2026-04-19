---
title: "buttons history"
description: "CLI reference for buttons history"
---

## buttons history

Show run history

### Synopsis

Show execution history for buttons.

Displays recent presses with status, exit code, duration, and timestamp.
Optionally filter by button name. Results are ordered most recent first.

**Examples:**

```bash
buttons history
buttons history deploy
buttons history deploy --last 5
buttons history --json
```

```
buttons history [button-name] [flags]
```

### Options

```
  -h, --help       help for history
      --last int   number of runs to show (default 20)
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

