---
title: "buttons install"
description: "CLI reference for buttons install"
---

## buttons install

Install a button (or every button with a tag) from a source

### Synopsis

Install buttons from a source into your buttons directory.

The argument is one of:

```text
  <name>            a single button (latest version)
  <name>@<version>  a pinned version
  tag:<tag>         every button in the source carrying <tag>
```

Each installed button's dependencies (its button.json "requires") are
installed too. Source + version + content hash are recorded in each
installed button.json for pinning and updates.

Source resolution, in order: --source `<dir>` / $BUTTONS_SOURCE (a local source),
else $BUTTONS_REGISTRY_URL (the hosted registry, bearer-authed with the
REGISTRY_KEY battery or $BUTTONS_BAT_REGISTRY_KEY).

**Examples:**

```bash
BUTTONS_REGISTRY_URL=https://registry.example buttons install @your-desk/hello
buttons install deploy --source ../button-source
buttons install deploy@1.2.0 --source ../button-source
```

```
buttons install <name | tag:x> [flags]
```

### Options

```
  -h, --help            help for install
      --source string   local source directory to install from (else $BUTTONS_REGISTRY_URL)
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

