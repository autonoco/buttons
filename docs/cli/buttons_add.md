---
title: "buttons add"
description: "CLI reference for buttons add"
---

## buttons add

Add a button dependency

### Synopsis

Add a registry button dependency to `.buttons/buttons.json` and install it.

Bare package names are not supported in the MVP. Use scoped names like
`@autono/hello`. Omit `@version` to track latest; include `@version` to pin an
exact immutable version.

**Examples:**

```bash
buttons add @your-desk/hello
buttons add @your-desk/hello@1.2.3
```

```
buttons add @desk/name[@version] [flags]
```

### Options

```
  -h, --help   help for add
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
```

### SEE ALSO

* [buttons](buttons.md)  - Deterministic workflow engine for agents
* [buttons install](buttons_install.md)  - Install buttons from .buttons/buttons.json
* [buttons status](buttons_status.md)  - Show available CLI and button updates
* [buttons update](buttons_update.md)  - Update the CLI and floating button dependencies
