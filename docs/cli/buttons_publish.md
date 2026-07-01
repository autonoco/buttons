---
title: "buttons publish"
description: "CLI reference for buttons publish"
---

## buttons publish

Publish a local button to the registry

### Synopsis

Publish a button. The button folder
(button.json + code + AGENTS.md, never its run history) is content-hashed and
uploaded so others can add and install it.

Publish uses $BUTTONS_REGISTRY_URL as the registry base URL. This repo does not
ship a default registry host; the caller must configure the target explicitly.

A registry publish takes a scoped name (@desk/name): the on-disk button is found
by its bare name, and @desk is its registry namespace. The registry pins
immutable versions; publish starts at the button's current version and auto-bumps
to the next number if that version already exists. Auth uses the *write* key
(REGISTRY_WRITE_KEY battery or $BUTTONS_BAT_REGISTRY_WRITE_KEY) — distinct from
the read key install uses.

**Examples:**

```bash
BUTTONS_REGISTRY_URL=https://registry.example buttons publish @your-desk/hello
```

```
buttons publish <name | @desk/name> [flags]
```

### Options

```
  -h, --help          help for publish
      --kind string   registry entry kind: button | drawer (default "button")
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

