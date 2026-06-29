---
title: "buttons publish"
description: "CLI reference for buttons publish"
---

## buttons publish

Publish a local button to a source so others can install it

### Synopsis

Publish a button — the inverse of 'buttons install'. The button folder
(button.json + code + AGENTS.md, never its run history) is content-hashed and
written to a source, where 'buttons install `<name>` --source `<dir>`' can fetch it.

The registry source (buttons.co, #275/#276) is not built yet; for now publish
to a local source directory with --source (or $BUTTONS_SOURCE). That directory
is a valid install source, so publish + install round-trip end-to-end today.

**Examples:**

```bash
buttons publish deploy --source ./pack
BUTTONS_SOURCE=./pack buttons publish deploy
```

```
buttons publish <name> [flags]
```

### Options

```
  -h, --help            help for publish
      --source string   source directory to publish to (until the registry lands, #276)
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

