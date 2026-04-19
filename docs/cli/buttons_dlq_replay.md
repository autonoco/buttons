---
title: "buttons dlq replay"
description: "CLI reference for buttons dlq replay"
---

## buttons dlq replay

Replay a DLQ entry (prints the original command to run)

### Synopsis

Print the command that would replay a DLQ entry. The actual
replay is intentionally left to the caller so agents can review the
inputs before re-running — the DLQ is a triage surface, not an
auto-retry daemon.

```
buttons dlq replay <id> [flags]
```

### Options

```
  -h, --help   help for replay
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons dlq](buttons_dlq.md)	 - Inspect and replay final-failed runs (dead letter queue)

