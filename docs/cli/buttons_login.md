---
title: "buttons login"
description: "CLI reference for buttons login"
---

## buttons login

Connect this machine to the Buttons platform

### Synopsis

Authorize this machine in your browser and store a publish token.

Opens your Buttons console to approve the connection under your organization,
then stores the issued token as the global REGISTRY_WRITE_KEY battery (used by
"buttons publish") and pins the registry URL as the REGISTRY_URL battery.

Revoke a machine any time from the console's Desks page.

```
buttons login [flags]
```

### Options

```
      --desk string       Buttons console URL that hosts the authorization page (default "https://desk.buttons.sh")
  -h, --help              help for login
      --no-browser        print the authorization URL instead of opening a browser
      --registry string   registry base URL the token is issued for (default "https://api.buttons.sh")
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

