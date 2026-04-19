---
title: "buttons update"
description: "CLI reference for buttons update"
---

## buttons update

Update buttons to the latest version

### Synopsis

Check for and install the latest version of buttons from GitHub Releases.

Downloads the correct archive for your OS and architecture, verifies
the SHA256 checksum, and atomically replaces the running binary.

**Examples:**

```bash
buttons update              # download and install the latest version
buttons update --check      # just check, don't install
buttons update --json       # structured output
```

```
buttons update [flags]
```

### Options

```
      --check   check for updates without installing
  -h, --help    help for update
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

