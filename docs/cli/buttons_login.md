---
title: "buttons login"
description: "CLI reference for buttons login"
---

## buttons login

Sign in to a registry with OAuth

### Synopsis

Sign in through the registry's standard OAuth/OIDC provider.

The CLI discovers the provider, opens an authorization-code + PKCE flow on a
127.0.0.1 loopback callback, and stores one credential envelope in the operating
system keychain. The CLI contains no provider-specific authentication code.

```
buttons login [flags]
```

### Options

```
  -h, --help                  help for login
      --no-browser            print the authorization URL instead of opening a browser
      --registry string       registry base URL (default "https://api.buttons.sh")
      --switch-organization   revoke the current login and select another organization
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

