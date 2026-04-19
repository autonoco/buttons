---
title: "buttons batteries list"
description: "CLI reference for buttons batteries list"
---

## buttons batteries list

List every battery

### Synopsis

List batteries from every scope. Values are redacted by default —
pass --reveal to print them in full.

Local entries that shadow a global key are shown after the global entry;
at press time the local value wins.

```
buttons batteries list [flags]
```

### Options

```
  -h, --help     help for list
      --reveal   print values in full instead of redacted
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons batteries](buttons_batteries.md)	 - Manage environment variables and secrets

