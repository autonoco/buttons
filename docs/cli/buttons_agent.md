---
title: "buttons agent"
description: "CLI reference for buttons agent"
---

## buttons agent

Register this workspace's device identity with the registry

### Synopsis

Manage this agent workspace's device identity.

The device holds an Ed25519 keypair generated on first use and stored 0600 in the
data dir (agent.json); the private key never leaves the machine. Identity is
proven by signature, not asserted.

All subcommands use $BUTTONS_REGISTRY_URL as the registry base URL — this repo
ships no default host. Enrollment consumes a one-time token supplied as the
ENROLL_TOKEN battery (or $BUTTONS_BAT_ENROLL_TOKEN).

```
buttons agent [flags]
```

### Options

```
  -h, --help   help for agent
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents
* [buttons agent enroll](buttons_agent_enroll.md)	 - Generate this device's key and bind it to its owner with a one-time token
* [buttons agent register](buttons_agent_register.md)	 - Register this workspace under a slug and print its URLs
* [buttons agent status](buttons_agent_status.md)	 - Show this device's identity (no secrets)

