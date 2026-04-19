---
title: "buttons summary"
description: "CLI reference for buttons summary"
---

## buttons summary

Print a workspace snapshot (buttons, drawers, recent runs)

### Synopsis

Print a workspace snapshot. Default output is a compact
pretty-printed table; --json returns a structured response suitable
for agents. --deep inlines full schemas and all recent runs.

```
buttons summary [flags]
```

### Options

```
      --deep   inline full schemas + all recent runs
  -h, --help   help for summary
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

