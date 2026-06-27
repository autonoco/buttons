---
title: "buttons import"
description: "CLI reference for buttons import"
---

## buttons import

Create buttons from external sources (skill, code, url)

### Synopsis

Import buttons from external sources:

  buttons import code <file>     wrap a script as a button (runtime inferred)
  buttons import skill <dir>     a button per script in an AgentSkills skill
  buttons import url <url>       create a button from a fetched HTTP spec
  buttons import mcp <server>    (planned) one button per MCP tool

Every import prints what it will create and asks to confirm. Pass --yes to
skip the prompt (required when running non-interactively).

### Options

```
  -h, --help          help for import
      --name string   override the generated button name (code/url)
  -y, --yes           skip the confirmation prompt
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents
* [buttons import code](buttons_import_code.md)	 - Wrap a script file as a button
* [buttons import mcp](buttons_import_mcp.md)	 - Create buttons from an MCP server's tools (planned)
* [buttons import skill](buttons_import_skill.md)	 - Create buttons from an AgentSkills skill directory
* [buttons import url](buttons_import_url.md)	 - Create a button from a spec fetched over HTTP

