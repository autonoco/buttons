---
title: "buttons install"
description: "CLI reference for buttons install"
---

## buttons install

Install buttons from `.buttons/buttons.json`

### Synopsis

Materialize the dependency manifest into `.buttons/buttons/` and refresh
`.buttons/buttons-lock.json`.

Use `buttons add @desk/name` to add a dependency. `buttons install` takes no
package argument; `buttons install @desk/name` fails with guidance to use
`buttons add`.

`buttons install` honors the lockfile for existing floating dependencies. To
move `"latest"` dependencies to newer published versions, use `buttons update`
or run `buttons add @desk/name` again.

**Examples:**

```bash
buttons install
buttons add @your-desk/hello
buttons add @your-desk/hello@1.2.3
```

```
buttons install [flags]
```

### Options

```
  -h, --help   help for install
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
```

### SEE ALSO

* [buttons](buttons.md)  - Deterministic workflow engine for agents
* [buttons add](buttons_add.md)  - Add a button dependency
* [buttons status](buttons_status.md)  - Show available CLI and button updates
* [buttons update](buttons_update.md)  - Update the CLI and floating button dependencies
