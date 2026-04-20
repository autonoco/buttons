---
title: "buttons webhook listen"
description: "CLI reference for buttons webhook listen"
---

## buttons webhook listen

Run the webhook listener — presses drawers when trigger paths are hit

### Synopsis

Runs a foreground HTTP listener exposed via Cloudflare tunnel so
drawers with webhook triggers get invoked when third-party services
POST to their paths.

Prereq:
  1. 'cloudflared' on PATH (brew install cloudflared)
  2. 'buttons webhook setup' run once for a stable named tunnel
     (quick-tunnel mode works too, but the URL changes each run — fine
     for dev, not for services that register a fixed URL up front)

Workflow:
  1. Create a drawer:               buttons drawer create on-apify-done
  2. Add steps that consume the webhook body via \${inputs.webhook.body.*}
  3. Attach a webhook trigger:      buttons drawer on-apify-done trigger webhook /apify
  4. Start the listener:            buttons webhook listen
  5. Configure the third-party service to POST to the printed URL.

When a POST arrives at a registered path:
  - The request body, headers, query, and method are materialized as
    \${inputs.webhook.body}, \${inputs.webhook.headers.*}, etc.
  - The drawer is pressed asynchronously — the HTTP response returns
    immediately with {ok, drawer} so the sender doesn't block on the
    full workflow.
  - If the drawer has a shared-token secret set, X-Buttons-Token (or
    ?token= query param) must match or the request is rejected 401.

The listener stays up until Ctrl-C. Run it in tmux, a separate pane,
or under launchd/systemd for always-on setups.

```
buttons webhook listen [flags]
```

### Options

```
  -h, --help        help for listen
      --no-tunnel   skip cloudflared; listen on 127.0.0.1 only (local testing)
      --port int    bind local listener on this port (0 = random)
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons webhook](buttons_webhook.md)	 - Expose a local URL via Cloudflare to receive webhook callbacks

