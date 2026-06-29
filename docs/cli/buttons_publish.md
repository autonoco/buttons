---
title: "buttons publish"
description: "CLI reference for buttons publish"
---

## buttons publish

Publish a local button to the registry (or a local source)

### Synopsis

Publish a button — the inverse of 'buttons install'. The button folder
(button.json + code + AGENTS.md, never its run history) is content-hashed and
uploaded so others can 'buttons install' it.

Targets, in order:

```text
  --source <dir> / $BUTTONS_SOURCE   a local source directory (dev round-trip)
  $BUTTONS_REGISTRY_URL              the hosted registry (publish @desk/name)
```

A registry publish takes a scoped name (@desk/name): the on-disk button is found
by its bare name, and @desk is its registry namespace. The registry pins
immutable versions, so button.json must carry a "version". Auth uses the *write*
key (REGISTRY_WRITE_KEY battery or $BUTTONS_BAT_REGISTRY_WRITE_KEY) — distinct
from the read key install uses.

**Examples:**

```bash
BUTTONS_REGISTRY_URL=https://registry.example buttons publish @your-desk/hello
buttons publish deploy --source ./pack
```

```
buttons publish <name | @desk/name> [flags]
```

### Options

```
  -h, --help            help for publish
      --kind string     registry entry kind: button | drawer (default "button")
      --source string   local source directory to publish to (else $BUTTONS_REGISTRY_URL)
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

