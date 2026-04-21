---
title: "buttons webhook setup"
description: "CLI reference for buttons webhook setup"
---

## buttons webhook setup

One-time Cloudflare login + named-tunnel config

```
buttons webhook setup [flags]
```

### Options

```
      --allow-apex              allow an apex hostname (e.g. example.com); DANGEROUS — overrides root DNS
      --api-account-id string   pin the CF account id when the token is authorized on several
      --api-token string        Cloudflare API token (recommended); or set $BUTTONS_CF_API_TOKEN
  -h, --help                    help for setup
      --hostname string         hostname for webhooks (e.g. webhooks.yourdomain.com)
      --overwrite-dns           replace any pre-existing Cloudflare DNS record at the hostname; DESTRUCTIVE
      --tunnel string           Cloudflare tunnel name (default "buttons")
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons webhook](buttons_webhook.md)	 - Expose a local URL via Cloudflare to receive webhook callbacks

