---
title: "buttons dlq"
description: "CLI reference for buttons dlq"
---

## buttons dlq

Inspect and replay final-failed runs (dead letter queue)

```
buttons dlq [flags]
```

### Options

```
  -h, --help   help for dlq
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents
* [buttons dlq list](buttons_dlq_list.md)	 - List final-failed runs
* [buttons dlq remove](buttons_dlq_remove.md)	 - Delete a DLQ entry (after out-of-band resolution)
* [buttons dlq replay](buttons_dlq_replay.md)	 - Replay a DLQ entry (prints the original command to run)

