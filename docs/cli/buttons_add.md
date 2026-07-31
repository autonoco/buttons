---
title: "buttons add"
description: "CLI reference for buttons add"
---

## buttons add

Add a registry package dependency

### Synopsis

Add a registry package dependency to .buttons/buttons.json and install it.

Bare package names are not supported in the MVP. Use scoped names like
@autono/hello. A package can be a button or a drawer; the registry index
declares its kind. Omit @version to track latest; include @version to pin an
exact immutable version.

By default, add re-resolves every floating "latest" dependency against the
registry while installing. Pass --no-refresh to install only the new package
and keep other floating dependencies at their locked versions.

**Examples:**

```bash
buttons add @your-desk/hello
buttons add @your-desk/hello@1
buttons add @your-desk/hello@1 --no-refresh
```

```
buttons add @desk/name[@version] [flags]
```

### Options

```
  -h, --help         help for add
      --no-refresh   keep other floating (latest) dependencies at their locked versions
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

