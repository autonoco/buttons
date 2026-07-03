---
title: "buttons agent register"
description: "CLI reference for buttons agent register"
---

## buttons agent register

Register this workspace under a slug and print its URLs

### Synopsis

Register this workspace: claim a slug, and receive its URL set from the
registry. Requires an enrolled device (run "buttons agent enroll" first).

--tunnel defaults to the tunnel id from a configured named webhook tunnel
(buttons webhook setup) when present.

```
buttons agent register [flags]
```

### Options

```
      --agent-id string    optional persona id
  -h, --help               help for register
      --principal string   optional principal this workspace serves
      --slug string        the workspace slug to claim (one DNS label)
      --tunnel string      tunnel id backing this workspace (defaults to the named webhook tunnel)
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons agent](buttons_agent.md)	 - Register this workspace's device identity with the registry

