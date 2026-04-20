---
title: "buttons"
description: "CLI reference for buttons"
---

## buttons

Deterministic workflow engine for agents

### Synopsis

Buttons gives agents deterministic escape hatches. Create, compose, and execute self-contained actions with clear inputs, outputs, and status.

```
buttons [flags]
```

### Options

```
  -h, --help       help for buttons
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons batteries](buttons_batteries.md)	 - Manage environment variables and secrets
* [buttons board](buttons_board.md)	 - Open the button board in a new terminal window
* [buttons config](buttons_config.md)	 - Read and write per-user settings
* [buttons create](buttons_create.md)	 - Create a new button
* [buttons delete](buttons_delete.md)	 - Delete a button
* [buttons drawer](buttons_drawer.md)	 - Manage drawer workflows (chains of buttons)
* [buttons history](buttons_history.md)	 - Show run history
* [buttons init](buttons_init.md)	 - Initialize a project-local .buttons directory
* [buttons list](buttons_list.md)	 - List all buttons
* [buttons logs](buttons_logs.md)	 - View past runs for a button or tail the live progress stream
* [buttons press](buttons_press.md)	 - Run a button
* [buttons smash](buttons_smash.md)	 - Run multiple buttons in parallel
* [buttons store](buttons_store.md)	 - Marketplace (search/install/import/publish)
* [buttons summary](buttons_summary.md)	 - Print a workspace snapshot (buttons, drawers, recent runs)
* [buttons tail](buttons_tail.md)	 - Follow the progress JSONL of a press
* [buttons update](buttons_update.md)	 - Update buttons to the latest version
* [buttons version](buttons_version.md)	 - Print build version, commit, and date

