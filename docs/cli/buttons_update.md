---
title: "buttons update"
description: "CLI reference for buttons update"
---

## buttons update

Update the CLI and floating package dependencies

### Synopsis

Install available updates for the buttons CLI and floating package dependencies.

The CLI binary is updated from GitHub Releases. Package dependencies are
refreshed from .buttons/buttons.json and .buttons/buttons-lock.json. Exact
versions are pins; update moves only dependencies requested as "latest".

Homebrew-managed installs are left to Homebrew by default. To let Buttons update
the CLI through Homebrew and run passive CLI binary updates when the throttle
allows, run:

```bash
buttons config set cli-auto-update true
```

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
