---
title: "buttons drawer"
description: "CLI reference for buttons drawer"
---

## buttons drawer

Manage drawer workflows (chains of buttons)

### Synopsis

Manage drawers — typed workflows that chain buttons with
${ref} references between steps.

Usage:
  buttons drawer create NAME
  buttons drawer list
  buttons drawer NAME add BUTTON [BUTTON ...]         append button step(s)
  buttons drawer NAME add drawer/OTHER                append a sub-drawer step
  buttons drawer NAME connect A to B                  auto-match output → args by name+type
  buttons drawer NAME connect A.output.x to B.args.y  explicit field path
  buttons drawer NAME set STEP.args.FIELD=value       write a literal or ${ref} into a step arg
  buttons drawer NAME press [key=value ...]           run it; unfilled required inputs go here
  buttons drawer NAME logs [--failed] [--limit N]     past runs for this drawer
  buttons drawer NAME remove                          delete the drawer
  buttons drawer NAME                                 summary (topology + validation + recent runs)
  buttons drawer schema                               print JSON Schema for drawer.json

Typical authoring flow:
  buttons drawer create deploy-flow
  buttons drawer deploy-flow add build publish
  buttons drawer deploy-flow connect build to publish
  buttons drawer deploy-flow set publish.args.env=prod
  buttons drawer deploy-flow press

```
buttons drawer [flags]
```

### Options

```
      --failed NAME logs   only return runs that failed (for NAME logs)
  -f, --follow NAME logs   stream live progress (for NAME logs)
  -h, --help               help for drawer
      --limit NAME logs    max runs to return (for NAME logs) (default 20)
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

