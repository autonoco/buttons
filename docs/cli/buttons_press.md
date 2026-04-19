---
title: "buttons press"
description: "CLI reference for buttons press"
---

## buttons press

Run a button

### Synopsis

Run a button by name.

Executes the action defined by the button, passing arguments as environment
variables (BUTTONS_ARG_`<NAME>`) for code buttons, or as template substitutions
for API buttons. Returns structured output in --json mode.

Common flags:
      --arg KEY=VALUE   pass an argument (repeatable; validated against the spec)
      --timeout SECS    override the button's configured timeout for this press
      --dry-run         print what would run without executing
      --json            emit machine-readable output (default when stdout is piped)

**Examples:**

```bash
buttons press deploy --arg env=production
buttons press weather --arg city=Miami --json
buttons press deploy --dry-run
buttons press slow-task --timeout 120
```

```
buttons press [name] [flags]
```

### Options

```
      --arg stringArray   argument as key=value
      --dry-run           show what would execute without running
  -h, --help              help for press
      --timeout int       override timeout in seconds
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

