---
title: "buttons trigger"
description: "CLI reference for buttons trigger"
---

## buttons trigger

Manage button triggers (cron, watch, webhook)

### Synopsis

Attach event triggers to a button so it presses automatically.

Triggers run under 'buttons serve':

```text
  - cron    fires on a schedule (5-field cron, in-process scheduler)
  - watch   fires when a file changes (polled, 500ms debounce)
  - webhook fires on an HTTP POST to a path on the serve listener
```

**Examples:**

```bash
buttons trigger add health --cron --schedule "0 */6 * * *"
buttons trigger add reindex --watch --path ./data.json
buttons trigger add deploy --webhook --webhook-path /hooks/deploy --token s3cr3t
buttons trigger list
buttons trigger rm health <trigger-id>
```

### Options

```
  -h, --help   help for trigger
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents
* [buttons trigger add](buttons_trigger_add.md)	 - Add a trigger to a button
* [buttons trigger list](buttons_trigger_list.md)	 - List triggers (all buttons, or one)
* [buttons trigger rm](buttons_trigger_rm.md)	 - Remove a trigger from a button

