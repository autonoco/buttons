---
title: "buttons status"
description: "CLI reference for buttons status"
---

## buttons status

Show available CLI and button updates

### Synopsis

Show whether the buttons CLI or installed registry buttons have updates.

Like every user-invoked command, status also enters the passive auto-update
gate before it prints. Use 'buttons update' to force an update immediately.

```
buttons status [flags]
```

### Options

```
  -h, --help   help for status
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents
