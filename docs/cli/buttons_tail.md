---
title: "buttons tail"
description: "CLI reference for buttons tail"
---

## buttons tail

Follow the progress JSONL of a press

### Synopsis

Tail a press's structured progress stream.

Argument can be either:
  • a button name — tails the LATEST press's progress file
  • an absolute path to a .progress.jsonl file

Scripts write to $BUTTONS_PROGRESS_PATH (set automatically by the
engine). Typical event shape:

    {"ts":"...","event":"progress","pct":0.25,"msg":"downloaded 5/20"}
    {"ts":"...","event":"log","level":"info","msg":"..."}

**Examples:**

```bash
buttons tail publish-npm              # latest press of publish-npm
buttons tail publish-npm -f           # follow forever
buttons tail /tmp/run.progress.jsonl  # specific file
```

```
buttons tail <button-or-path> [flags]
```

### Options

```
  -f, --follow   keep tailing as new lines arrive
  -h, --help     help for tail
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

