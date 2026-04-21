---
name: buttons
description: |
  Deterministic workflow engine for AI agents. Create and press
  reusable buttons (shell scripts, HTTP APIs, prompt instructions)
  with typed inputs and structured JSON output. Use when wrapping
  repeatable actions, calling HTTP endpoints, or building multi-step
  workflows where each step is a named, typed, pressable button.
license: Apache-2.0
compatibility: Requires the buttons CLI binary installed (go install github.com/autonoco/buttons@latest or curl installer).
metadata:
  author: autonoco
  repository: https://github.com/autonoco/buttons
---

# Buttons CLI

Deterministic workflow engine for AI agents. Create reusable, composable actions with typed inputs and structured JSON output.

## When to use

- Wrap a repeatable action (shell script, HTTP API call, prompt instruction) as a named, typed, pressable button
- Get structured JSON output from shell commands or HTTP endpoints
- Create self-documenting actions that other agents can discover and press
- Build multi-step workflows where each step is a button with typed args

## Quick reference

```bash
# Default: scaffold + edit + press. Creates main.sh with placeholder you edit.
buttons create deploy --arg env:string:required
# → edit ~/.buttons/buttons/deploy/main.sh, then: buttons press deploy --arg env=staging

# Shortcuts for known content (skip the scaffold):
buttons create greet --code 'echo "Hello, $BUTTONS_ARG_NAME"' --arg name:string:required
buttons create weather --url 'https://wttr.in/{{city}}?format=j1' --arg city:string:required
buttons create deploy-checklist --prompt "Verify: tests pass, staging green, team notified"
buttons create etl -f ./scripts/transform.sh --arg source:string:required

# Press buttons
buttons press weather --arg city=Miami
buttons press weather --arg city=Miami --json
buttons press deploy --dry-run

# Discover and manage
buttons list
buttons weather              # detail view for a single button
buttons history weather
buttons delete weather
buttons version --json
```

## Commands

### `buttons`

Deterministic workflow engine for agents

```
buttons [command]
```

| Flag | Type | Description |
|------|------|-------------|
| `--json` | bool | output in JSON format |
| `--no-input` | bool | disable all interactive prompts |
| `--summary` | bool | show a read-only plan/snapshot instead of mutating |

### `buttons batteries`

Manage environment variables and secrets

```
buttons batteries [command]
```

#### `buttons batteries get`

Print a battery value

```
buttons batteries get
```

#### `buttons batteries list`

List every battery

```
buttons batteries list [flags]
```

| Flag | Type | Description |
|------|------|-------------|
| `--reveal` | bool | print values in full instead of redacted |

#### `buttons batteries rm`

Remove a battery

```
buttons batteries rm [flags]
```

| Flag | Type | Description |
|------|------|-------------|
| `--global` | bool | target the global batteries file (~/.buttons/batteries.json) |
| `--local` | bool | target the project-local batteries file (.buttons/batteries.json) |

#### `buttons batteries set`

Set a battery

```
buttons batteries set [flags]
```

| Flag | Type | Description |
|------|------|-------------|
| `--global` | bool | target the global batteries file (~/.buttons/batteries.json) |
| `--local` | bool | target the project-local batteries file (.buttons/batteries.json) |

### `buttons board`

Open the button board in a new terminal window

```
buttons board [flags]
```

| Flag | Type | Description |
|------|------|-------------|
| `--inline` | bool | render the board in the current terminal instead of spawning a new window |

### `buttons config`

Read and write per-user settings

```
buttons config [command]
```

#### `buttons config set`

Set a setting

```
buttons config set
```

#### `buttons config unset`

Clear a setting (revert to built-in default)

```
buttons config unset
```

### `buttons create`

Create a new button

```
buttons create [flags]
```

| Flag | Type | Description |
|------|------|-------------|
| `--allow-private-networks` | bool | allow --url buttons to reach private network addresses (localhost, 10/8, 172.16/12, 192.168/16, 169.254/16, IPv6 private ranges). Required for local dev targets. |
| `--arg` | stringArray | argument definition (name:type:required|optional) |
| `--body` | string | HTTP request body (supports {{arg}} templates) |
| `--code` | string | inline script code (shortcut for one-liners) |
| `-d, --description` | string | button description |
| `-f, --file` | string | copy an existing script file into the button folder |
| `--header` | stringArray | HTTP header as 'Key: Value' (repeatable) |
| `--ignore` | bool | add this button to .buttons/.gitignore so git won't track it (good for scratch/test buttons) |
| `--max-response-size` | string | max HTTP response body size for --url buttons (e.g. 10M, 1G). default: 10M |
| `--method` | string | HTTP method for --url (default: GET) |
| `--prompt` | string | prompt/instruction for the consuming agent (written to AGENT.md) |
| `--runtime` | string | code runtime: shell, python, node (default: shell) |
| `--timeout` | int | execution timeout in seconds |
| `--url` | string | HTTP API endpoint URL (supports {{arg}} templates) |

### `buttons delete`

Delete a button

```
buttons delete [flags]
```

| Flag | Type | Description |
|------|------|-------------|
| `-F, --force` | bool | delete without confirmation |

### `buttons drawer`

Manage drawer workflows (chains of buttons)

```
buttons drawer
```

### `buttons history`

Show run history

```
buttons history [flags]
```

| Flag | Type | Description |
|------|------|-------------|
| `--last` | int | number of runs to show |

### `buttons ignore`

Keep a button or drawer out of git (writes .buttons/.gitignore)

```
buttons ignore
```

### `buttons init`

Initialize a project-local .buttons directory

```
buttons init [flags]
```

| Flag | Type | Description |
|------|------|-------------|
| `--agent` | stringSlice | agent integrations to install (cursor,claude,cline,copilot,agents-md); 'none' skips |

### `buttons list`

List all buttons

```
buttons list
```

### `buttons logs`

View past runs for a button or tail the live progress stream

```
buttons logs [flags]
```

| Flag | Type | Description |
|------|------|-------------|
| `--failed` | bool | only return runs that failed |
| `-f, --follow` | bool | stream the latest press's progress events live (agent-friendly, no TUI) |
| `--limit` | int | max runs to return |

### `buttons press`

Run a button

```
buttons press [flags]
```

| Flag | Type | Description |
|------|------|-------------|
| `--arg` | stringArray | argument as key=value |
| `--dry-run` | bool | show what would execute without running |
| `-f, --follow` | bool | stream stdout/stderr to stderr as the press runs (final Result still goes to stdout) |
| `--idempotency-key` | string | reuse the cached result for this key if present (cross-run dedup) |
| `--idempotency-ttl` | duration | how long idempotency entries stay valid (e.g. 1h, 24h) |
| `--timeout` | int | override timeout in seconds |

### `buttons smash`

Run multiple buttons in parallel

```
buttons smash
```

### `buttons store`

Marketplace (search/install/import/publish)

```
buttons store
```

### `buttons summary`

Print a workspace snapshot (buttons, drawers, recent runs)

```
buttons summary [flags]
```

| Flag | Type | Description |
|------|------|-------------|
| `--deep` | bool | inline full schemas + all recent runs |

### `buttons tail`

Follow the progress JSONL of a press

```
buttons tail [flags]
```

| Flag | Type | Description |
|------|------|-------------|
| `-f, --follow` | bool | keep tailing as new lines arrive |

### `buttons unignore`

Re-include a previously-ignored button or drawer in git

```
buttons unignore
```

### `buttons update`

Update buttons to the latest version

```
buttons update [flags]
```

| Flag | Type | Description |
|------|------|-------------|
| `--check` | bool | check for updates without installing |

### `buttons version`

Print build version, commit, and date

```
buttons version
```

### `buttons webhook`

Expose a local URL via Cloudflare to receive webhook callbacks

```
buttons webhook [command]
```

#### `buttons webhook listen`

Run the webhook listener — presses drawers when trigger paths are hit

```
buttons webhook listen [flags]
```

| Flag | Type | Description |
|------|------|-------------|
| `--no-tunnel` | bool | skip cloudflared; listen on 127.0.0.1 only (local testing) |
| `--port` | int | bind local listener on this port (0 = random) |

#### `buttons webhook logout`

Clear named-tunnel config (falls back to quick mode)

```
buttons webhook logout
```

#### `buttons webhook setup`

One-time Cloudflare login + named-tunnel config

```
buttons webhook setup [flags]
```

| Flag | Type | Description |
|------|------|-------------|
| `--allow-apex` | bool | allow an apex hostname (e.g. example.com); DANGEROUS — overrides root DNS |
| `--api-account-id` | string | pin the CF account id when the token is authorized on several |
| `--api-token` | string | Cloudflare API token (recommended); or set $BUTTONS_CF_API_TOKEN |
| `--hostname` | string | hostname for webhooks (e.g. webhooks.yourdomain.com) |
| `--overwrite-dns` | bool | replace any pre-existing Cloudflare DNS record at the hostname; DESTRUCTIVE |
| `--tunnel` | string | Cloudflare tunnel name |

#### `buttons webhook status`

Show current webhook mode and URL

```
buttons webhook status
```

#### `buttons webhook test`

Round-trip verify: spin up a tunnel, self-POST, observe delivery

```
buttons webhook test
```

## JSON output contract

Every command supports `--json`. Piped/non-TTY output auto-detects and switches to JSON.

Success:
```json
{"ok": true, "data": { ... }}
```

Error:
```json
{"ok": false, "error": {"code": "NOT_FOUND", "message": "button not found: deploy"}}
```

Error codes: `NOT_FOUND`, `MISSING_ARG`, `VALIDATION_ERROR`, `TIMEOUT`, `SCRIPT_ERROR`, `RUNTIME_MISSING`, `INTERNAL_ERROR`, `NOT_IMPLEMENTED`.

## Argument types

Define at create time: `--arg name:type:required|optional`

| Type | Values | Example |
|------|--------|---------|
| `string` | Any text | `--arg city:string:required` |
| `int` | Integer | `--arg count:int:optional` |
| `bool` | `true`/`false`/`1`/`0` | `--arg verbose:bool:optional` |

Pass at press time: `--arg key=value`

- **Code buttons:** args become `BUTTONS_ARG_<NAME>` environment variables
- **HTTP buttons:** args substitute into `{{arg}}` URL/body templates (context-aware encoded)

## Button sources

`buttons create <name>` scaffolds a shell button with a placeholder `main.sh` the agent edits, then presses. Use a shortcut flag to skip the scaffold:

| Flag | Source | Runtime |
|------|--------|--------|
| (none) | Scaffold `main.<ext>` with shebang + TODO | `--runtime shell\|python\|node` (default: shell) |
| `--code` | Inline script body (one-liners) | Same as above |
| `-f`/`--file` | Existing script file (copied into button folder) | Detected from shebang |
| `--url` | HTTP endpoint with `{{arg}}` templates | HTTP client |
| `--prompt` | Instruction for the consuming agent | Returns text, no execution |

`--prompt` can be combined with any other source as a modifier.

## Common patterns

### Create, press, inspect lifecycle
```bash
buttons create check-health --url 'https://api.example.com/health' -d "Health check"
buttons press check-health --json
buttons check-health         # detail view: args, last run, usage examples
buttons history check-health  # all past runs
```

### Code button with prompt context
```bash
buttons create check-logs \
  --code 'tail -100 /var/log/app.log' \
  --prompt "Summarize any errors or warnings from these logs"
```

The `prompt` field appears in `--json` output so the calling agent knows what to do with the stdout.

### HTTP button hitting a local dev server
```bash
buttons create local-api \
  --url 'http://localhost:3000/api/{{endpoint}}' \
  --arg endpoint:string:required \
  --allow-private-networks
```

`--allow-private-networks` is required for localhost/private-IP targets (blocked by default for SSRF protection).

## Storage

All data lives under `~/.buttons/` (override with `BUTTONS_HOME`):

```
~/.buttons/buttons/<name>/
  button.json     # spec (args, runtime, timeout)
  main.sh         # code file (.sh, .py, .js)
  AGENT.md        # prompt instruction
  pressed/        # run history as JSON files
```
