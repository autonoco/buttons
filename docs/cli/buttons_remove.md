---
title: "buttons remove"
description: "CLI reference for buttons remove"
---

## buttons remove

Remove a registry package dependency

### Synopsis

Remove a registry package dependency added with 'buttons add'.

Remove drops the dependency from .buttons/buttons.json, deletes the
installed package directory, and cleans .buttons/buttons-lock.json. Only
directories the installer materialized (stamped with install state) are
deleted; a package another installed package still requires keeps its
files until its last dependent is removed too.

Use 'buttons delete' to delete a locally created button.

**Examples:**

```bash
buttons remove @your-desk/hello
buttons remove @your-desk/hello --json
```

```
buttons remove @desk/name [flags]
```

### Options

```
  -h, --help   help for remove
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

