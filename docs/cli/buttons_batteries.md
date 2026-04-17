---
title: "buttons batteries"
description: "CLI reference for buttons batteries"
---

## buttons batteries

Manage environment variables and secrets

### Synopsis

Batteries are key/value pairs injected into every button press
as BUTTONS_BAT_`<KEY>`=<value>. Use them to store API tokens and other
secrets outside your button scripts.

Scopes:
  global   ~/.buttons/batteries.json  — available from every project
  local    .buttons/batteries.json    — only when pressing inside the
                                        project; overrides global on
                                        key collision

List / get read from both scopes (local overrides on collision). Set /
rm target local when inside a project, global otherwise; pass --global
or --local to pick explicitly.

Keys must match [A-Z][A-Z0-9_]* (uppercase, digits, underscore).

**Examples:**

```bash
buttons batteries set APIFY_TOKEN apify_api_xxx
buttons batteries set OPENAI_KEY sk-... --global
buttons batteries list
buttons batteries get APIFY_TOKEN
buttons batteries rm OLD_KEY
```

```
buttons batteries [flags]
```

### Options

```
  -h, --help   help for batteries
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents
* [buttons batteries get](buttons_batteries_get.md)	 - Print a battery value
* [buttons batteries list](buttons_batteries_list.md)	 - List every battery
* [buttons batteries rm](buttons_batteries_rm.md)	 - Remove a battery
* [buttons batteries set](buttons_batteries_set.md)	 - Set a battery

