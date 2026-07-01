---
title: "buttons update"
description: "CLI reference for buttons update"
---

## buttons update

Update the CLI and installed registry buttons

### Synopsis

Install available updates for the buttons CLI and installed registry buttons.

The CLI binary is updated from GitHub Releases. Installed buttons are refreshed
from the source stamped in each installed button.json.

**Examples:**

```bash
buttons update
buttons update --json
```

```
buttons update [flags]
```

### Options

```
  -h, --help   help for update
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

