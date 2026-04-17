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

A reference guide is written to .buttons/AGENT.md so any coding
agent that opens the folder can learn what Buttons is and how to
use it.

Interactively (on a TTY), init also offers to install a Buttons
skill file for your coding agent (Cursor, Claude Code, Cline,
GitHub Copilot, or a generic AGENTS.md). None is installed without
explicit selection.

**Examples:**

```bash
cd my-project
buttons init
buttons init --agent cursor,agents-md   # non-interactive selection
buttons init --agent none                # skip the skill picker
buttons create deploy --code './scripts/deploy.sh' --arg env:string:required
```

```
buttons init [flags]
```

### Options

```
      --agent strings   agent integrations to install (cursor,claude,cline,copilot,agents-md); 'none' skips
  -h, --help            help for init
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

