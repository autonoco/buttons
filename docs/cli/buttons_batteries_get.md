---
title: "buttons batteries get"
description: "CLI reference for buttons batteries get"
---

## buttons batteries get

Print a battery value

### Synopsis

Print the raw value of a battery to stdout. Intended for shell
substitution, e.g. `curl -H "Authorization: Bearer $(buttons batteries get APIFY_TOKEN)" ...`.

Looks up local first (if in a project), then global. In --json mode the
value is still returned raw — redaction only applies to `list`.

```
buttons batteries get KEY [flags]
```

### Options

```
  -h, --help   help for get
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons batteries](buttons_batteries.md)	 - Manage environment variables and secrets

