# Buttons

[![CI](https://github.com/autonoco/buttons/actions/workflows/ci.yml/badge.svg)](https://github.com/autonoco/buttons/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)
[![Release](https://img.shields.io/github/v/release/autonoco/buttons)](https://github.com/autonoco/buttons/releases)

n8n for agents. A CLI workflow engine where AI agents build and run their own automations.

Each button is a self-contained, reusable action. Create it once, press it forever. Buttons wraps code, APIs, and agent instructions into a single interface with typed args and structured output.

## Install

Buttons ships as a single static binary for **macOS** and **Linux** (amd64 + arm64). Windows support is tracked in [autonoco/autono#350](https://github.com/autonoco/autono/issues/350).

### curl (macOS / Linux)

```bash
curl -fsSL https://raw.githubusercontent.com/autonoco/buttons/main/install.sh | sh
```

Installs to `/usr/local/bin` by default (uses `sudo` if needed). The script verifies the SHA256 checksum of every download. Override with env vars:

```bash
curl -fsSL https://raw.githubusercontent.com/autonoco/buttons/main/install.sh | BUTTONS_VERSION=v0.70.0 sh
curl -fsSL https://raw.githubusercontent.com/autonoco/buttons/main/install.sh | BUTTONS_INSTALL_DIR=$HOME/.local/bin sh
```

### Homebrew

```bash
brew install autonoco/tap/buttons
```

Tap is auto-updated by goreleaser on every release. `brew upgrade buttons` to update.
Homebrew installs shell completions automatically, so tab completion works without a separate setup step.

### Go

```bash
go install github.com/autonoco/buttons@latest
```

Installs to `$(go env GOPATH)/bin` (usually `~/go/bin`).

### npm / pnpm / bun

```bash
npm install -g @autono/buttons
```

Thin JS shim that resolves the platform binary via `optionalDependencies`.

### Docker

```bash
docker pull ghcr.io/autonoco/buttons:latest
docker run --rm -v $PWD/.buttons:/home/buttons/.buttons \
  ghcr.io/autonoco/buttons:latest press weather --arg city=Miami
```

Multi-arch Alpine image, ~5 MB, published to GHCR. Mount a volume to persist state between invocations.

## Updating

```bash
buttons status           # show available CLI/content updates
buttons update           # install available CLI/content updates
buttons update --json    # structured output
```

`buttons update` checks GitHub Releases for the CLI and refreshes floating button dependencies from `.buttons/buttons.json` and `.buttons/buttons-lock.json`. Use `buttons status` for a read-only check. Homebrew installs are auto-detected — you'll be told to `brew upgrade buttons` instead. Docker users re-pull the image.

## Verify the installation

```bash
buttons version
buttons --version
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

Buttons can wrap code, HTTP APIs, imported files, or prompt-only instructions. Use `--prompt` as a modifier on any runnable button to attach instructions for the consuming agent.

### Code

```bash
# Scaffold + edit (default). Creates main.sh with a placeholder, tells you where to edit it.
buttons create deploy --arg env:string:required
# Then edit ~/.buttons/buttons/deploy/main.sh and press:
buttons press deploy --arg env=staging

# Or skip the scaffold with --code for one-liners
buttons create greet --code 'echo "Hello, $BUTTONS_ARG_NAME!"' --arg name:string:required

# Python scaffold
buttons create transform --runtime python --arg input:string:required

# Node scaffold
buttons create parse --runtime node

# Or import an existing script file
buttons create etl --file ./scripts/etl.sh --arg source:string:required
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

The `--prompt` flag attaches an instruction for the consuming agent. Use it standalone or combine it with code/API buttons.

```bash
# Standalone: just an instruction
buttons create deploy-checklist \
  --prompt "Before deploying, verify: 1) All tests pass 2) Staging is green 3) Team notified"

# Code + prompt: run the command, agent knows what to do with the output
buttons create check-logs \
  --code 'northflank logs --service api --env production --tail 100' \
  --prompt "Summarize any errors or warnings from these logs"

# API + prompt: call the API, agent interprets the result
buttons create analyze-weather \
  --url 'https://wttr.in/{{city}}?format=j1' \
  --arg city:string:required \
  --prompt "Extract temperature and conditions as a one-line summary"
```

When pressed, the result includes both the output and the `prompt`:

```json
{
  "ok": true,
  "data": {
    "status": "ok",
    "stdout": "ERROR 2026-04-18 connection timeout to db-primary...",
    "prompt": "Summarize any errors or warnings from these logs",
    "button": "check-logs"
  }
}
```

## Pressing Buttons

```bash
buttons press weather --arg city=Miami
buttons press weather --arg city=Miami --json
buttons press weather --arg city=Miami --dry-run   # validate args + render command, don't execute
buttons press slow-task --timeout 120
```

### Arguments

```bash
# Define at creation
buttons create deploy --code '...' --arg env:string:required --arg verbose:bool:optional

# Pass at press time
buttons press deploy --arg env=production --arg verbose=true
```

Types: `string`, `int`, `bool`, `enum`. Enum declares a fixed set of valid values — the CLI rejects anything outside the set and the board renders a picker:

```bash
buttons create deploy --code '...' --arg env:enum:required:staging|prod|canary
```

Code buttons get args as `BUTTONS_ARG_<NAME>` env vars. API buttons use `{{arg}}` template substitution.

## Workflows

Drawers chain buttons into repeatable workflows. Use them when one action needs multiple steps, data from an earlier step, loops, waits, or a triggerable wrapper.

```bash
buttons drawer create standup-digest
buttons drawer standup-digest add github-activity granola-query slack-post
buttons drawer standup-digest connect github-activity to slack-post
buttons drawer standup-digest set slack-post.args.channel='#standup'
buttons drawer standup-digest press
```

Step outputs are available to later steps through `${step_id.output.field}` refs when the upstream button prints JSON.

```bash
buttons drawer release-flow set publish.args.version='${build.output.version}'
```

Drawers also support sub-drawers, loops, switches, aggregates, waits, webhook triggers, validation summaries, error handlers, and their own run history.

## Triggers

Triggers are the planned unified surface for running buttons and drawers automatically:

```bash
buttons trigger webhook linkedin-sync /linkedin-sync
buttons trigger cron slack-sync "0 */2 * * *"
buttons trigger hook docs-sync --enable "file:docs/**/*.md"
```

Webhook, cron, and hook triggers all target a button or drawer. Button targets can compile down to hidden one-step drawer wrappers so the runtime keeps one workflow model internally.

Current implementation: webhook-triggered drawers are live today with:

```bash
buttons drawer linkedin-sync trigger webhook /linkedin-sync
buttons webhook listen
```

The unified `buttons trigger ...` command is the planned surface over the same drawer model.

## Registry Install Contract

Registry installs use a package-manager style manifest and lockfile:

```bash
buttons add @your-desk/hello        # writes .buttons/buttons.json and installs latest
buttons add @your-desk/deploy@1     # pins an exact version
buttons install                     # materializes .buttons/buttons.json
buttons status                      # reports available CLI/content updates
buttons update                      # updates floating dependencies
```

The root manifest is committed:

```json
{
  "schema_version": 1,
  "dependencies": {
    "@your-desk/hello": "latest",
    "@your-desk/deploy": "1"
  }
}
```

`"latest"` floats and can move when `buttons update`, passive OTA, or a publish-triggered wake runs. Exact versions are pins and do not move unless you change the manifest with another `buttons add @desk/name@version`.

`.buttons/buttons-lock.json` stores the exact resolved version, content hash, installed local name, kind, and resolution time. Installed runtime specs live in `.buttons/buttons/<name>/button.json`; they do not need `source`, `source_name`, or `content_hash` for update resolution.

Install from the hosted registry:

```bash
BUTTONS_REGISTRY_URL=https://registry.example buttons add @your-desk/hello
BUTTONS_REGISTRY_URL=https://registry.example buttons add @your-desk/hello@1.2.0
BUTTONS_REGISTRY_URL=https://registry.example buttons install
```

Version flow:

```bash
# Publisher creates and publishes immutable version 1.
buttons create hello --code 'echo hello'
BUTTONS_REGISTRY_URL=https://registry.example buttons publish @your-desk/hello

# Agent workspace tracks latest and installs that published version.
BUTTONS_REGISTRY_URL=https://registry.example buttons add @your-desk/hello

# Publisher changes the source button and publishes again. If version 1 already
# exists, publish bumps button.json to version 2 and retries automatically.
BUTTONS_REGISTRY_URL=https://registry.example buttons publish @your-desk/hello

# The agent workspace can see and apply the new version.
buttons status
buttons update
```

Required peer buttons from `button.json` `requires` are installed transitively from the registry. `requires` uses the same shape as the root manifest: scoped package names mapped to `"latest"` or an exact version.

## REST API Server

Expose local buttons over HTTP with the same press contract as the CLI:

```bash
buttons serve
buttons serve --host 0.0.0.0 --api-key "$(openssl rand -hex 16)"
```

Endpoints include `GET /api/buttons`, `GET /api/buttons/{name}`, `POST /api/buttons/{name}/press`, and `GET /api/buttons/{name}/runs`. HTTP buttons are gated behind `--allow-http-buttons`.

## Discovering Buttons

```bash
buttons                    # interactive card-grid board in a TTY, plain table when piped
buttons <name>             # full-screen detail page (args, last run, script); press `e` to edit
buttons logs <name>        # live log viewer — follow mode with `f`, jump with `g`/`G`, `Esc` to exit
buttons list --json        # machine-readable list
buttons summary --json     # workspace snapshot for agents
```

On the board, pressing a button with required arguments opens an inline form instead of erroring out. Fill it in, hit Enter, the press fires.

## Configuration

Personal defaults live in `~/.buttons/settings.json`. Manage them with `buttons config`:

```bash
buttons config                                # show current values
buttons config set default-timeout 600        # default for future `buttons create`
buttons config set theme amber                # board TUI theme: default | paper | phosphor | amber
buttons config unset theme                    # revert to built-in default
```

Env var overrides: `BUTTONS_HOME` (relocate the data directory), `BUTTONS_THEME` (one-shot theme override — useful for A/B).

## Secrets (Batteries)

Batteries are key/value pairs injected into every button press as `BUTTONS_BAT_<KEY>=<value>`. Use them to keep API tokens out of button scripts.

```bash
buttons batteries set APIFY_TOKEN apify_api_xxx       # local by default inside a project, global otherwise
buttons batteries set OPENAI_KEY sk-... --global      # ~/.buttons/batteries.json
buttons batteries list
buttons batteries get APIFY_TOKEN
buttons batteries rm OLD_KEY
```

Local (`.buttons/batteries.json`) overrides global on key collisions. Keys must match `[A-Z][A-Z0-9_]*`.

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
    2026-04-18T09-53-45.json
```

Override storage location with `BUTTONS_HOME`.

## JSON Output

Every command supports `--json`. Piped output auto-detects non-TTY and outputs JSON automatically.

```json
{"ok": true, "data": {"status": "ok", "stdout": "...", "prompt": "...", "button": "weather"}}
{"ok": false, "error": {"code": "MISSING_ARG", "message": "...", "hint": "...", "spec": [...]}}
```

Error codes: `NOT_FOUND`, `MISSING_ARG`, `VALIDATION_ERROR`, `TIMEOUT`, `SCRIPT_ERROR`, `RUNTIME_MISSING`, `STORAGE_ERROR`, `NOT_APPLICABLE`, `INTERNAL_ERROR`, `NOT_IMPLEMENTED`.

## Create Flags

By default, `buttons create <name>` scaffolds a shell button with a placeholder `main.sh` the agent can edit. Shortcut flags below let you skip the placeholder.

| Flag | Short | Description |
|------|-------|-------------|
| `--runtime` | | Scaffold runtime: shell, python, node (default: shell) |
| `--code` | | Inline script body (shortcut for one-liners) |
| `--file` | `-f` | Copy an existing script file into the button folder |
| `--url` | | HTTP API endpoint (supports `{{arg}}` templates) |
| `--method` | | HTTP method (default: GET) |
| `--header` | | HTTP header as `Key: Value` (repeatable) |
| `--body` | | HTTP request body (supports `{{arg}}` templates) |
| `--prompt` | | Prompt instruction written to AGENT.md (standalone or modifier on any source) |
| `--arg` | | Argument: `name:type:required\|optional` (enums: `name:enum:required:a\|b\|c`) |
| `--description` | `-d` | Button description |
| `--timeout` | | Execution timeout in seconds (default: 300) |
| `--max-response-size` | | Max HTTP response body for `--url` buttons, e.g. `10M`, `1G` (default: `10M`) |
| `--allow-private-networks` | | Allow `--url` buttons to reach private network addresses (localhost, 10/8, 172.16/12, 192.168/16, 169.254/16). Required for local dev targets. |

## Security

- Every execution runs under `context.WithTimeout` (default 300s)
- Process group kill: SIGTERM then SIGKILL after 5s grace
- Args passed as env vars, never interpolated into shell
- HTTP buttons block private networks by default and cap response bodies

See [SECURITY.md](SECURITY.md) for the full threat model and how to report vulnerabilities.

## Documentation

Full CLI reference, concepts, and changelog live in the [`docs/`](docs/) directory (published via Mintlify).

## License

Buttons is licensed under the [Apache License, Version 2.0](LICENSE).

Copyright 2026 Darley Ventures LLC dba Autono.
