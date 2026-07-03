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

* [buttons add](buttons_add.md)	 - Add a registry package dependency
* [buttons agent](buttons_agent.md)	 - Register this workspace's device identity with the registry
* [buttons batteries](buttons_batteries.md)	 - Manage environment variables and secrets
* [buttons board](buttons_board.md)	 - Open the button board in a new terminal window
* [buttons config](buttons_config.md)	 - Read and write per-user settings
* [buttons create](buttons_create.md)	 - Create a new button
* [buttons delete](buttons_delete.md)	 - Delete a button
* [buttons drawer](buttons_drawer.md)	 - Manage drawer workflows (chains of buttons)
* [buttons history](buttons_history.md)	 - Show run history
* [buttons ignore](buttons_ignore.md)	 - Keep a button or drawer out of git (writes .buttons/.gitignore)
* [buttons import](buttons_import.md)	 - Create buttons from external sources (skill, code, url)
* [buttons init](buttons_init.md)	 - Initialize a project-local .buttons directory
* [buttons install](buttons_install.md)	 - Install packages from .buttons/buttons.json
* [buttons list](buttons_list.md)	 - List all buttons
* [buttons logs](buttons_logs.md)	 - View past runs for a button or tail the live progress stream
* [buttons mcp](buttons_mcp.md)	 - Run an MCP server over stdio (expose buttons to agents)
* [buttons press](buttons_press.md)	 - Run a button
* [buttons publish](buttons_publish.md)	 - Publish a local button or drawer to the registry
* [buttons serve](buttons_serve.md)	 - Run a REST API server exposing buttons over HTTP
* [buttons smash](buttons_smash.md)	 - Run multiple buttons in parallel
* [buttons status](buttons_status.md)	 - Show available CLI and package updates
* [buttons summary](buttons_summary.md)	 - Print a workspace snapshot (buttons, drawers, recent runs)
* [buttons tail](buttons_tail.md)	 - Follow the progress JSONL of a press
* [buttons trigger](buttons_trigger.md)	 - Manage button triggers (cron, watch, webhook)
* [buttons unignore](buttons_unignore.md)	 - Re-include a previously-ignored button or drawer in git
* [buttons update](buttons_update.md)	 - Update the CLI and floating package dependencies
* [buttons version](buttons_version.md)	 - Print build version, commit, and date
* [buttons webhook](buttons_webhook.md)	 - Expose a local URL via Cloudflare to receive webhook callbacks

