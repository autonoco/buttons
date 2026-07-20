---
title: "buttons logout"
description: "CLI reference for buttons logout"
---

## buttons logout

Disconnect this machine from the Buttons platform

### Synopsis

Remove the stored publish token and registry URL.

The token itself stays valid until revoked from the console's Desks page —
logout only forgets it on this machine.

```
buttons logout [flags]
```

### Options

```
  -h, --help   help for logout
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

