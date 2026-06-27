---
title: "buttons serve"
description: "CLI reference for buttons serve"
---

## buttons serve

Run a REST API server exposing buttons over HTTP

### Synopsis

Start an HTTP server that exposes your buttons as a REST API, using the
same structured-output contract and execution path as the CLI.

Endpoints:
  GET  /api/health               liveness (no auth)
  GET  /api/buttons              list all buttons
  GET  /api/buttons/{name}       a button's spec
  POST /api/buttons/{name}/press execute a button (JSON body: {"args":{…},"timeout":N})
  GET  /api/buttons/{name}/runs  recent run history

Auth: every endpoint except /api/health requires 'Authorization: Bearer <key>'.
The key comes from --api-key, else the 'API_KEY' battery, else $BUTTONS_API_KEY.
With no key the server runs auth-free and therefore binds to loopback only —
binding a non-loopback host without a key is refused.

Buttons created via the CLI while the server runs are picked up immediately
(state is read from disk per request).

**Examples:**

```bash
buttons serve
buttons serve --port 3000
buttons serve --host 0.0.0.0 --api-key "$(openssl rand -hex 16)"
```

```
buttons serve [flags]
```

### Options

```
      --allow-http-buttons   allow pressing http (outbound-request) buttons over the API (SSRF surface; off by default)
      --api-key string       bearer key required on all endpoints (else the 'API_KEY' battery or $BUTTONS_API_KEY)
  -h, --help                 help for serve
      --host string          host/interface to bind (use 0.0.0.0 to expose; requires an API key) (default "127.0.0.1")
      --port int             port to listen on (default 8080)
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

