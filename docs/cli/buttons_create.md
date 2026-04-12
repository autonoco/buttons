---
title: "buttons create"
description: "CLI reference for buttons create"
---

## buttons create

Create a new button

### Synopsis

Create a new button from a script file, inline code, or API endpoint.

A button wraps a single action with typed arguments, a timeout, and
structured output. Provide --file for a script, --code for inline code,
or --url for an HTTP API endpoint.

Arguments are defined with --arg in name:type:required|optional format.
Supported types: string, int, bool. Args are injected as env vars for
scripts or substituted into URL templates for API buttons.

**Examples:**

```bash
buttons create deploy -f ./scripts/deploy.sh --arg env:string:required
buttons create greet --code 'echo "Hello, $BUTTONS_ARG_NAME"' --arg name:string:required
buttons create weather --url 'https://wttr.in/{{city}}?format=j1' --arg city:string:required
buttons create webhook --url https://api.example.com/hook --method POST
buttons create graphql --url https://api.example.com/graphql --method POST \
  --header "Content-Type: application/json" --body '{"query": "{ viewer { login } }"}'
buttons create check-logs --prompt "Use the Northflank CLI to read production logs and summarize errors"
```

```
buttons create [name] [flags]
```

### Options

```
      --allow-private-networks     allow --url buttons to reach private network addresses (localhost, 10/8, 172.16/12, 192.168/16, 169.254/16, IPv6 private ranges). Required for local dev targets.
      --arg stringArray            argument definition (name:type:required|optional)
      --body string                HTTP request body (supports {{arg}} templates)
      --code string                inline script code
      --code-stdin                 read code from stdin
  -d, --description string         button description
  -f, --file string                path to script file
      --header stringArray         HTTP header as 'Key: Value' (repeatable)
  -h, --help                       help for create
      --max-response-size string   max HTTP response body size for --url buttons (e.g. 10M, 1G). default: 10M
      --method string              HTTP method for --url (default: GET)
      --prompt string              prompt/instruction for the consuming agent (written to AGENT.md)
      --runtime string             code runtime: shell, python, node (default: shell)
      --timeout int                execution timeout in seconds (default 60)
      --url string                 HTTP API endpoint URL (supports {{arg}} templates)
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

