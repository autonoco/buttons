---
title: "buttons agent setup"
description: "CLI reference for buttons agent setup"
---

## buttons agent setup

Register this agent under a slug and print its URLs (enrolls on first run)

### Synopsis

Set up this agent's identity and public URL. One idempotent command:
generates the device key on first use, enrolls with the ENROLL_TOKEN battery if
the device isn't bound yet, then registers the slug and prints its URLs. Safe to
re-run — it re-points to the current tunnel.

--tunnel is optional. When omitted, the tunnel is taken from a configured named
webhook tunnel (buttons webhook setup), then this agent's previously provisioned
tunnel; if there's still none, the broker provisions one and returns its
run-token (saved to agent.json).

```
buttons agent setup <slug> [flags]
```

### Options

```
  -h, --help               help for setup
      --principal string   optional principal this agent serves
      --tunnel string      tunnel id backing this agent (defaults to the named webhook tunnel)
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons agent](buttons_agent.md)	 - Set up this agent's identity and public URL

