---
title: "buttons webhook"
description: "CLI reference for buttons webhook"
---

## buttons webhook

Expose a local URL via Cloudflare to receive webhook callbacks

### Synopsis

Drawer steps occasionally need to register a public URL with a third-
party service (Apify, GitHub, Stripe) and wait for it to post back. The
webhook subsystem manages a Cloudflare tunnel to your local machine so
that URL works without hosting anything yourself.

Two modes:

  quick   zero setup. Each webhook step spawns a fresh Quick Tunnel
          (ephemeral *.trycloudflare.com URL). Good when the service
          accepts a per-run webhook URL.

  named   stable hostname on your own Cloudflare account. Set up once
          via 'buttons webhook setup'. Required when a service wants a
          fixed URL registered up front (e.g. GitHub webhooks).

Prereq: 'cloudflared' binary on PATH. Install via 'brew install cloudflared'.

Verbs:
  buttons webhook setup    — one-time: Cloudflare login + pick a hostname
  buttons webhook status   — show current mode + hostname
  buttons webhook test     — end-to-end round-trip verify
  buttons webhook listen   — run the dispatcher that presses triggered drawers
  buttons webhook logout   — forget the named-tunnel config (quick mode again)

```
buttons webhook [flags]
```

### Options

```
  -h, --help   help for webhook
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents
* [buttons webhook listen](buttons_webhook_listen.md)	 - Run the webhook listener — presses drawers when trigger paths are hit
* [buttons webhook logout](buttons_webhook_logout.md)	 - Clear named-tunnel config (falls back to quick mode)
* [buttons webhook setup](buttons_webhook_setup.md)	 - One-time Cloudflare login + named-tunnel config
* [buttons webhook status](buttons_webhook_status.md)	 - Show current webhook mode and URL
* [buttons webhook test](buttons_webhook_test.md)	 - Round-trip verify: spin up a tunnel, self-POST, observe delivery

