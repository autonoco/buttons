---
title: "buttons status"
description: "CLI reference for buttons status"
---

## buttons status

Show available CLI and button updates

### Synopsis

Show whether the buttons CLI or manifest dependencies have updates.

Like every user-invoked command, status also enters enabled passive update
paths before it prints. buttons-auto-update may refresh floating button
dependencies; cli-auto-update may update the CLI binary when the throttle
allows. Use 'buttons update' to force the full update path immediately.

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

