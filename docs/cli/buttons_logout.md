---
title: "buttons logout"
description: "CLI reference for buttons logout"
---

## buttons logout

Revoke registry OAuth credentials and sign out

### Synopsis

Revoke both the bounded publish capability and the OAuth refresh token,
then remove the credential envelope from the operating system keychain.

```
buttons logout [flags]
```

### Options

```
  -h, --help              help for logout
      --registry string   registry base URL (defaults to the configured registry)
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

