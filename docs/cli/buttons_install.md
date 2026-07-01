---
title: "buttons install"
description: "CLI reference for buttons install"
---

## buttons install

Install buttons from .buttons/buttons.json

### Synopsis

Install buttons declared in .buttons/buttons.json.

Use 'buttons add @desk/name' to add a dependency. Use 'buttons install'
to materialize the dependency manifest into .buttons/buttons/ and refresh
.buttons/buttons-lock.json.

**Examples:**

```bash
buttons install
buttons add @your-desk/hello
buttons add @your-desk/hello@1
```

```
buttons install [flags]
```

### Options

```
  -h, --help   help for install
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

