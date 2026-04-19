---
title: "buttons create"
description: "CLI reference for buttons create"
---

## buttons create

Create a new button

### Synopsis

Create a new button.

By default, 'buttons create <name>' scaffolds a shell button with a
placeholder main.sh the agent can edit, then press. Use --runtime to
scaffold a Python or Node button instead.

Provide a shortcut flag to skip the placeholder: --code for a one-line
inline script, --file to copy an existing script, --url for an HTTP
endpoint, or --prompt for a standalone instruction.

Arguments are defined with --arg in name:type:required|optional format.
Supported types: string, int, bool, enum.

Enum args accept a 4th pipe-separated segment listing the allowed
values — the TUI press form renders them as a horizontal choice row,
and the CLI validates the supplied value is in the set:

  --arg env:enum:required:staging|prod|canary

Args are injected as env vars for scripts or substituted into URL
templates for API buttons.

Common flags:
  -f, --file PATH       copy an existing script file as this button's code
      --code STRING     inline script body (shortcut for one-liners)
      --url URL         turn this into an HTTP button
      --arg SPEC        define an arg (name:type:required|optional,
                        or name:enum:required:a|b|c; repeatable)
      --timeout SECS    execution timeout (default: 300)
  -d, --description S   human-readable description for 'buttons list'
      --runtime NAME    shell | python | node  (default: shell)

**Examples:**

```bash
buttons create deploy --arg env:enum:required:staging|prod|canary   # enum arg
buttons create deploy                                  # scaffold, then edit main.sh
buttons create etl --runtime python                    # scaffold, then edit main.py
buttons create greet --code 'echo "Hello, $BUTTONS_ARG_NAME"' --arg name:string:required
buttons create k8s-deploy -f ./scripts/deploy.sh --arg env:string:required
buttons create weather --url 'https://wttr.in/{{city}}?format=j1' --arg city:string:required
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
      --code string                inline script code (shortcut for one-liners)
  -d, --description string         button description
  -f, --file string                copy an existing script file into the button folder
      --header stringArray         HTTP header as 'Key: Value' (repeatable)
  -h, --help                       help for create
      --max-response-size string   max HTTP response body size for --url buttons (e.g. 10M, 1G). default: 10M
      --method string              HTTP method for --url (default: GET)
      --prompt string              prompt/instruction for the consuming agent (written to AGENT.md)
      --runtime string             code runtime: shell, python, node (default: shell)
      --timeout int                execution timeout in seconds (default 300)
      --url string                 HTTP API endpoint URL (supports {{arg}} templates)
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

