---
title: "buttons ignore"
description: "CLI reference for buttons ignore"
---

## buttons ignore

Keep a button or drawer out of git (writes .buttons/.gitignore)

### Synopsis

Adds the named button/drawer to .buttons/.gitignore so git
won't track it. Useful for scratch/test buttons an agent spins up
while iterating.

  buttons ignore NAME                — ignore a button
  buttons ignore drawer/NAME         — ignore a drawer
  buttons ignore                     — list currently-ignored entries
  buttons unignore NAME              — re-include in git
  buttons create NAME --ignore       — create + ignore in one step

The .buttons/.gitignore file is a standard gitignore scoped to the
.buttons/ subtree — git applies it natively, no extra config.

```
buttons ignore [name] [flags]
```

### Options

```
  -h, --help   help for ignore
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

