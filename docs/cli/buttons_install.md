---
title: "buttons install"
description: "CLI reference for buttons install"
---

## buttons install

Install a button (or every button with a tag) from a source

### Synopsis

Install buttons from a source into your buttons directory.

The argument is one of:
  <name>            a single button (latest version)
  <name>@<version>  a pinned version
  tag:<tag>         every button in the source carrying <tag>

Each installed button's dependencies (its button.json "requires") are
installed too. Source + version + content hash are recorded in each
installed button.json for pinning and updates.

The registry source (buttons.co, #275) is not built yet; for now pass a
local source directory with --source (or $BUTTONS_SOURCE).

**Examples:**

```bash
buttons install deploy --source ./pack
buttons install tag:autono-cal --source ./pack
buttons install deploy@1.2.0 --source ./pack
```

```
buttons install <name | tag:x> [flags]
```

### Options

```
  -h, --help            help for install
      --source string   source directory to install from (until the registry lands, #275)
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

