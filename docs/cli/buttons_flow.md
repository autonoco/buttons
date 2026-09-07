---
title: "buttons flow"
description: "CLI reference for buttons flow"
---

## buttons flow

Buttons Flow — pressable boards for drawer_kind:flow

### Synopsis

Flow boards are drawer_kind:flow definitions that compile to an ordinary
drawer pipeline on every press. Verbs here are sugar over press/history/summary.

### Options

```
  -h, --help   help for flow
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents
* [buttons flow approve](buttons_flow_approve.md)	 - Approve a gated transition
* [buttons flow ensure-buttons](buttons_flow_ensure-buttons.md)	 - Install shared flow pipeline buttons for a provider
* [buttons flow init](buttons_flow_init.md)	 - Register triggers and prove a flow board presses
* [buttons flow logs](buttons_flow_logs.md)	 - Recent press history for a board
* [buttons flow reject](buttons_flow_reject.md)	 - Reject a gated transition
* [buttons flow rm](buttons_flow_rm.md)	 - Remove webhook trigger and schedule for a board
* [buttons flow status](buttons_flow_status.md)	 - Board status summary
* [buttons flow task](buttons_flow_task.md)	 - Task CRUD against a board's provider

