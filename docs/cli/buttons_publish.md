---
title: "buttons publish"
description: "CLI reference for buttons publish"
---

## buttons publish

Publish a local button or drawer to the registry

### Synopsis

Publish a local package. A package can be a button
(.buttons/buttons/`<name>`/button.json + code + AGENTS.md) or a drawer
(.buttons/drawers/`<name>`/drawer.json + AGENTS.md). Run history under pressed/
is never published.

Publish uses $BUTTONS_REGISTRY_URL when set, otherwise it uses the registry URL
pinned by "buttons login". This repo does not ship a default registry host.

A registry publish takes a scoped name (@desk/name): the on-disk package is
found by its bare name, and @desk is its registry namespace. The CLI detects
whether the local package is a button or drawer from button.json or drawer.json.
The registry pins immutable versions; publish starts at the package's current
version and auto-bumps to the next number if that version already exists. Auth
uses either the explicit machine/CI key in $BUTTONS_BAT_REGISTRY_WRITE_KEY or
the human OAuth credential stored by "buttons login" in the OS keychain.

**Examples:**

```bash
BUTTONS_REGISTRY_URL=https://registry.example buttons publish @your-desk/hello
BUTTONS_REGISTRY_URL=https://registry.example buttons publish @your-desk/my-pack
```

```
buttons publish <name | @desk/name> [flags]
```

### Options

```
  -h, --help   help for publish
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

