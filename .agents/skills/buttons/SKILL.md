# Buttons CLI

Deterministic workflow engine for AI agents. Create reusable, composable actions with typed inputs and structured JSON output.

## When to use

- Wrap a repeatable action (shell script, HTTP API call, agent instruction) as a named, typed, pressable button
- Get structured JSON output from shell commands or HTTP endpoints
- Create self-documenting actions that other agents can discover and press
- Build multi-step workflows where each step is a button with typed args

## Quick reference

```bash
# Create buttons
buttons create greet --code 'echo "Hello, $BUTTONS_ARG_NAME"' --arg name:string:required
buttons create weather --url 'https://wttr.in/{{city}}?format=j1' --arg city:string:required
buttons create deploy-checklist --agent "Verify: tests pass, staging green, team notified"
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

### `buttons batteries`

Manage environment variables and secrets

```
buttons batteries [command]
```

#### `buttons batteries list`

List all environment variables

```
buttons batteries list
```

#### `buttons batteries rm`

Remove an environment variable

```
buttons batteries rm
```

#### `buttons batteries set`

Set an environment variable

```
buttons batteries set
```

### `buttons board`

Show the button board (TUI dashboard)

```
buttons board
```

### `buttons create`

Create a new button

```
buttons create [flags]
```

| Flag | Type | Description |
|------|------|-------------|
| `--agent` | string | agent instruction/system prompt |
| `--allow-private-networks` | bool | allow --url buttons to reach private network addresses (localhost, 10/8, 172.16/12, 192.168/16, 169.254/16, IPv6 private ranges). Required for local dev targets. |
| `--arg` | stringArray | argument definition (name:type:required|optional) |
| `--body` | string | HTTP request body (supports {{arg}} templates) |
| `--code` | string | inline script code |
| `--code-stdin` | bool | read code from stdin |
| `-d, --description` | string | button description |
| `-f, --file` | string | path to script file |
| `--header` | stringArray | HTTP header as 'Key: Value' (repeatable) |
| `--max-response-size` | string | max HTTP response body size for --url buttons (e.g. 10M, 1G). default: 10M |
| `--method` | string | HTTP method for --url (default: GET) |
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

Manage button groups (drawers)

```
buttons drawer [command]
```

#### `buttons drawer add`

Add a button to a drawer

```
buttons drawer add
```

#### `buttons drawer create`

Create a new drawer

```
buttons drawer create
```

#### `buttons drawer list`

List all drawers

```
buttons drawer list
```

### `buttons history`

Show run history

```
buttons history [flags]
```

| Flag | Type | Description |
|------|------|-------------|
| `--last` | int | number of runs to show |

### `buttons list`

List all buttons

```
buttons list
```

### `buttons press`

Run a button

```
buttons press [flags]
```

| Flag | Type | Description |
|------|------|-------------|
| `--arg` | stringArray | argument as key=value |
| `--dry-run` | bool | show what would execute without running |
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

### `buttons version`

Print build version, commit, and date

```
buttons version
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

| Flag | Source | Runtime |
|------|--------|--------|
| `--code` | Inline script | `--runtime shell\|python\|node` (default: shell) |
| `--code-stdin` | Piped script from stdin | Same as `--code` |
| `-f`/`--file` | Existing script file (copied into button folder) | Detected from shebang |
| `--url` | HTTP endpoint with `{{arg}}` templates | HTTP client |
| `--agent` | Instruction for the consuming agent | Returns text, no execution |

`--agent` can be combined with any other source as a modifier.

## Common patterns

### Create, press, inspect lifecycle
```bash
buttons create check-health --url 'https://api.example.com/health' -d "Health check"
buttons press check-health --json
buttons check-health         # detail view: args, last run, usage examples
buttons history check-health  # all past runs
```

### Code button with agent context
```bash
buttons create check-logs \
  --code 'tail -100 /var/log/app.log' \
  --agent "Summarize any errors or warnings from these logs"
```

The `agent_prompt` field appears in `--json` output so the calling agent knows what to do with the stdout.

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
  AGENT.md        # agent instruction
  pressed/        # run history as JSON files
```
