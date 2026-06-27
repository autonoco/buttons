---
title: "buttons mcp"
description: "CLI reference for buttons mcp"
---

## buttons mcp

Run an MCP server over stdio (expose buttons to agents)

### Synopsis

Start a Model Context Protocol server on stdio so an agent (e.g. Claude
Code) can discover and press buttons as tools.

Uses a thin meta-tool surface — buttons_list, buttons_press, buttons_inspect
(and buttons_create with --allow-create) — instead of one tool per button, so
large button sets don't degrade the MCP client.

Security:
  - Only buttons with "mcp_enabled": true are listed, pressable, or inspectable.
  - Args are validated against the button's spec and passed as BUTTONS_ARG_`<NAME>`
    env vars — never substituted into shell text.
  - Per button: max 10 calls/min, 1 concurrent press, hard 120s timeout cap.
  - buttons_create is OFF unless --allow-create is passed.

stdout carries only protocol messages; logs go to stderr. Register with an
agent, e.g. Claude Code:
  claude mcp add buttons -- buttons mcp

```
buttons mcp [flags]
```

### Options

```
      --allow-create   expose the buttons_create tool (off by default)
  -h, --help           help for mcp
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

