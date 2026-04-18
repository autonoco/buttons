---
title: "buttons config"
description: "CLI reference for buttons config"
---

## buttons config

Read and write per-user settings

### Synopsis

Manage per-user defaults stored in ~/.buttons/settings.json.

Settings are global only — personal preferences shouldn't leak into
project repos via .buttons/. Project-level knobs live on each button
(e.g. 'buttons create --timeout N' pins a timeout for that button
specifically).

Known keys:
  default-timeout   seconds used when 'buttons create' is run without
                    an explicit --timeout flag (fallback: 300)

Running `buttons config` with no subcommand prints the current values.

**Examples:**

```bash
buttons config
buttons config set default-timeout 600
buttons config unset default-timeout
```

```
buttons config [flags]
```

### Options

```
  -h, --help   help for config
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents
* [buttons config set](buttons_config_set.md)	 - Set a setting
* [buttons config unset](buttons_config_unset.md)	 - Clear a setting (revert to built-in default)

