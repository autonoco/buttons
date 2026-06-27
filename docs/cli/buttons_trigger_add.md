---
title: "buttons trigger add"
description: "CLI reference for buttons trigger add"
---

## buttons trigger add

Add a trigger to a button

```
buttons trigger add <button> [flags]
```

### Options

```
      --arg stringArray       argument key=value passed when the trigger fires (repeatable)
      --cron                  cron-scheduled trigger
  -h, --help                  help for add
      --path string           file path to watch
      --schedule string       cron schedule, 5-field (e.g. "0 */6 * * *")
      --token string          shared secret required on webhook POSTs (X-Buttons-Token or ?token=)
      --watch                 file-watch trigger
      --webhook               webhook (HTTP POST) trigger
      --webhook-path string   URL path for the webhook (e.g. /hooks/deploy)
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons trigger](buttons_trigger.md)	 - Manage button triggers (cron, watch, webhook)

