---
title: "buttons delete"
description: "CLI reference for buttons delete"
---

## buttons delete

Delete a button

### Synopsis

Delete a button and all its history.

Prompts for confirmation unless --force is passed. In JSON or non-TTY
mode, confirmation is skipped automatically (agents are non-interactive).

**Examples:**

```bash
buttons delete deploy
buttons delete deploy -F
buttons delete deploy --json
```

```
buttons delete [name] [flags]
```

### Options

```
  -F, --force   delete without confirmation
  -h, --help    help for delete
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

