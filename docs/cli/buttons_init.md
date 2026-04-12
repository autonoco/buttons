---
title: "buttons init"
description: "CLI reference for buttons init"
---

## buttons init

Initialize a project-local .buttons directory

### Synopsis

Create a .buttons/ directory in the current working directory so
buttons are scoped to this project instead of the global ~/.buttons/.

Project-local buttons are discovered automatically: any buttons
command run inside this directory (or a subdirectory) will use the
local .buttons/ folder. Buttons created here won't appear when you
run commands from other projects.

The global ~/.buttons/ is still used as a fallback when no project-
local .buttons/ exists in the directory tree.

A .gitignore is created inside .buttons/ to exclude run history
(pressed/) from version control while keeping button specs, code
files, and agent instructions committed.

**Examples:**

```bash
cd my-project
buttons init
buttons create deploy --code './scripts/deploy.sh' --arg env:string:required
# → button lives in my-project/.buttons/buttons/deploy/
```

```
buttons init [flags]
```

### Options

```
  -h, --help   help for init
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

