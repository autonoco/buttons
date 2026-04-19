---
title: "buttons version"
description: "CLI reference for buttons version"
---

## buttons version

Print build version, commit, and date

### Synopsis

Print the build version, commit SHA, build date, Go toolchain,
and OS/architecture that this buttons binary was built with.

**Examples:**

```bash
buttons version
buttons version --json
buttons --version        # terse, Cobra-builtin flag form
```

```
buttons version [flags]
```

### Options

```
  -h, --help   help for version
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

