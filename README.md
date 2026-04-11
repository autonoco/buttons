# Buttons

[![CI](https://github.com/autonoco/buttons/actions/workflows/ci.yml/badge.svg)](https://github.com/autonoco/buttons/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)
[![Release](https://img.shields.io/github/v/release/autonoco/buttons)](https://github.com/autonoco/buttons/releases)

n8n for agents. A CLI workflow engine where AI agents build and run their own automations.

Each button is a self-contained, reusable action. Create it once, press it forever. Buttons wraps code, APIs, and agent instructions into a single interface with typed args and structured output.

## Install

Buttons ships as a single static binary for **macOS** and **Linux** (amd64 + arm64). Windows support is tracked in [autonoco/autono#350](https://github.com/autonoco/autono/issues/350).

### curl (macOS / Linux)

The fastest path for a single machine:

```bash
curl -fsSL https://raw.githubusercontent.com/autonoco/buttons/main/install.sh | sh
```

Defaults install to `/usr/local/bin` and use `sudo` if that directory isn't writable. Pin a specific version or change the install location with env vars:

```bash
# Pin a version
curl -fsSL https://raw.githubusercontent.com/autonoco/buttons/main/install.sh | BUTTONS_VERSION=v0.1.0 sh

# Install to a user-owned directory (no sudo)
curl -fsSL https://raw.githubusercontent.com/autonoco/buttons/main/install.sh | BUTTONS_INSTALL_DIR=$HOME/.local/bin sh
```

The script verifies the SHA256 checksum of every download against the release's `checksums.txt` before installing.

### Docker (for deployed agents)

Buttons is published as a multi-arch image on GitHub Container Registry:

```bash
docker pull ghcr.io/autonoco/buttons:latest
docker run --rm ghcr.io/autonoco/buttons:latest version
```

Most agent deployments want the binary baked into their own image. Use a multi-stage copy:

```dockerfile
FROM python:3.12-slim AS agent
# ... your agent setup ...

# Buttons binary, ~5 MB, no runtime dependencies
COPY --from=ghcr.io/autonoco/buttons:v0.1.0 /usr/local/bin/buttons /usr/local/bin/buttons
```

The image is Alpine-based with `/bin/sh` so shell buttons work out of the box. Derived images can `apk add python3 nodejs` when those runtimes are needed for other button types.

For a one-off invocation with state persisted to the host:

```bash
docker run --rm -v $PWD/.buttons:/home/buttons/.buttons \
  ghcr.io/autonoco/buttons:latest press weather --arg city=Miami
```

### Go

If you already have a Go toolchain:

```bash
go install github.com/autonoco/buttons@latest
```

Installs to `$(go env GOPATH)/bin` (usually `~/go/bin`). Make sure that's on your `$PATH`.

### Homebrew — coming soon

A Homebrew tap is tracked in [autonoco/autono#351](https://github.com/autonoco/autono/issues/351). When it lands:

```bash
brew install autonoco/tap/buttons
```

## Updating

Buttons does not ship a `buttons update` command — each install channel has its own upgrade path:

| Installed via | Update with |
|---|---|
| `curl \| sh` | Re-run the same `curl \| sh` command |
| Docker | `docker pull ghcr.io/autonoco/buttons:latest` |
| `go install` | `go install github.com/autonoco/buttons@latest` |
| Homebrew (once available) | `brew upgrade buttons` |

## Verify the installation

```bash
buttons version
buttons version --json
buttons --version    # shorter Cobra-built-in form
```

## Quick Start

```bash
# Create a button from an API
buttons create weather --url 'https://wttr.in/{{city}}?format=j1' --arg city:string:required -d "get weather"

# See what it does
buttons weather

# Press it
buttons press weather --arg city=Miami

# See the history
buttons history weather
```

## Creating Buttons

Four ways to create buttons. Use `--agent` as a modifier on any of them to attach an instruction for the consuming agent.

### Code

```bash
# Shell (default)
buttons create greet --code 'echo "Hello, $BUTTONS_ARG_NAME!"' --arg name:string:required

# Python
buttons create transform --runtime python --code 'import json; print(json.dumps({"ok": True}))'

# Node
buttons create parse --runtime node --code 'console.log(JSON.stringify({status: "ok"}))'

# Multi-line via stdin
buttons create etl --code-stdin <<'EOF'
curl -s $BUTTONS_ARG_SOURCE | jq '.data[]'
EOF
```

### API

```bash
# GET with URL templates
buttons create weather --url 'https://wttr.in/{{city}}?format=j1' --arg city:string:required

# POST with JSON body
buttons create notify --url https://hooks.slack.com/services/xxx \
  --method POST \
  --header "Content-Type: application/json" \
  --body '{"text": "{{message}}"}' \
  --arg message:string:required

# GraphQL
buttons create repos --url https://api.github.com/graphql \
  --method POST \
  --header "Authorization: Bearer {{token}}" \
  --header "Content-Type: application/json" \
  --body '{"query": "{ repository(owner: \"{{owner}}\", name: \"{{repo}}\") { stargazerCount } }"}' \
  --arg token:string:required --arg owner:string:required --arg repo:string:required
```

### File

Import an existing script. The file is copied into the button folder.

```bash
buttons create deploy -f ./scripts/deploy.sh --arg env:string:required
```

### Agent (standalone or modifier)

The `--agent` flag attaches an instruction for the consuming agent. Use it standalone or combine it with code/API buttons.

```bash
# Standalone: just an instruction
buttons create deploy-checklist \
  --agent "Before deploying, verify: 1) All tests pass 2) Staging is green 3) Team notified"

# Code + agent: run the command, agent knows what to do with the output
buttons create check-logs \
  --code 'northflank logs --service api --env production --tail 100' \
  --agent "Summarize any errors or warnings from these logs"

# API + agent: call the API, agent interprets the result
buttons create analyze-weather \
  --url 'https://wttr.in/{{city}}?format=j1' \
  --arg city:string:required \
  --agent "Extract temperature and conditions as a one-line summary"
```

When pressed, the result includes both the output and the `agent_prompt`:

```json
{
  "ok": true,
  "data": {
    "status": "ok",
    "stdout": "ERROR 2026-03-31 connection timeout to db-primary...",
    "agent_prompt": "Summarize any errors or warnings from these logs",
    "button": "check-logs"
  }
}
```

## Pressing Buttons

```bash
buttons press weather --arg city=Miami
buttons press weather --arg city=Miami --json
buttons press weather --arg city=Miami --dry-run
buttons press slow-task --timeout 120
```

### Arguments

```bash
# Define at creation
buttons create deploy --code '...' --arg env:string:required --arg verbose:bool:optional

# Pass at press time
buttons press deploy --arg env=production --arg verbose=true
```

Types: `string`, `int`, `bool`. Code buttons get args as `BUTTONS_ARG_<NAME>` env vars. API buttons use `{{arg}}` template substitution.

## Discovering Buttons

```bash
# Board view (all buttons)
buttons

# Detail view (single button with args, usage, last run)
buttons weather

# JSON list
buttons list --json
```

## History

Every press is recorded as a JSON file in the button's `pressed/` folder.

```bash
buttons history
buttons history weather
buttons history --last 5
buttons history weather --json
```

## Deleting Buttons

```bash
buttons delete deploy
buttons delete deploy -F          # skip confirmation
buttons delete deploy --json      # JSON mode implies force
buttons rm deploy                 # rm is an alias for delete
```

## Button Folder Structure

Each button is a self-contained folder:

```
~/.buttons/buttons/weather/
  button.json     # spec (args, runtime, timeout)
  main.sh         # code file (.sh, .py, .js based on runtime)
  AGENT.md        # agent instruction/context
  pressed/        # run history
    2026-03-31T09-53-45.json
```

Override storage location with `BUTTONS_HOME` environment variable.

## JSON Output

Every command supports `--json`. Piped output auto-detects non-TTY and outputs JSON automatically.

```json
{"ok": true, "data": {"status": "ok", "stdout": "...", "agent_prompt": "...", "button": "weather"}}
{"ok": false, "error": {"code": "MISSING_ARG", "message": "...", "hint": "...", "spec": [...]}}
```

Error codes: `NOT_FOUND`, `MISSING_ARG`, `VALIDATION_ERROR`, `TIMEOUT`, `SCRIPT_ERROR`, `RUNTIME_MISSING`, `INTERNAL_ERROR`, `NOT_IMPLEMENTED`.

## Create Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--code` | | Inline script code |
| `--code-stdin` | | Read code from piped stdin |
| `--runtime` | | Code runtime: shell, python, node (default: shell) |
| `--url` | | HTTP API endpoint (supports `{{arg}}` templates) |
| `--method` | | HTTP method (default: GET) |
| `--header` | | HTTP header as `Key: Value` (repeatable) |
| `--body` | | HTTP request body (supports `{{arg}}` templates) |
| `--file` | `-f` | Import a script file (copied into button folder) |
| `--agent` | | Agent instruction (standalone or modifier on any source) |
| `--arg` | | Argument: `name:type:required\|optional` |
| `--description` | `-d` | Button description |
| `--timeout` | | Execution timeout in seconds (default: 60) |
| `--max-response-size` | | Max HTTP response body for `--url` buttons, e.g. `10M`, `1G` (default: `10M`) |
| `--allow-private-networks` | | Allow `--url` buttons to reach private network addresses (localhost, 10/8, 172.16/12, 192.168/16, 169.254/16). Required for local dev targets. |

## Security

- `context.WithTimeout` on every execution (default 60s)
- Process group kill: SIGTERM then SIGKILL after 5s grace
- Args as env vars, never interpolated into shell
- File permissions: 0600 on specs, 0700 on code files
- URL `{{arg}}` values are context-aware encoded (`url.PathEscape` in paths, `url.QueryEscape` in queries, JSON-escaped in bodies)
- HTTP response bodies are capped (default 10 MB, per-button configurable)
- HTTP buttons block private network addresses by default; opt in per-button with `--allow-private-networks`

See [SECURITY.md](SECURITY.md) for the full threat model and how to report vulnerabilities privately.

## License

Buttons is licensed under the [Apache License, Version 2.0](LICENSE).

Copyright 2026 Darley Ventures LLC dba Autono.
