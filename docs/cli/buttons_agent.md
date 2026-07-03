---
title: "buttons agent"
description: "CLI reference for buttons agent"
---

## buttons agent

Set up this agent's identity and public URL

### Synopsis

Set up and inspect this agent workspace's identity and public URL.

The device holds an Ed25519 keypair generated on first use and stored 0600 in the
data dir (agent.json); the private key never leaves the machine. Identity is
proven by signature, not asserted.

Uses $BUTTONS_REGISTRY_URL as the registry base URL (this repo ships no default
host). First-time setup consumes a one-time token from the ENROLL_TOKEN battery
(or $BUTTONS_BAT_ENROLL_TOKEN).

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
* [buttons agent setup](buttons_agent_setup.md)	 - Register this agent under a slug and print its URLs (enrolls on first run)
* [buttons agent status](buttons_agent_status.md)	 - Show this device's identity (no secrets)

