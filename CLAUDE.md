# Buttons CLI — Claude Instructions

## What This Is

Buttons is a CLI workflow engine where AI agents are the primary users. Think n8n, but the user is an agent building automations from a terminal instead of a human dragging nodes on a canvas.

Agents use Buttons to create reusable actions (buttons), chain them into workflows (drawers), and run them on demand. A button can wrap a script file, custom code in any language (shell, Python, JS), an MCP tool call, or an agent task with injected system instructions. The runtime varies but the interface is always the same: typed input, execute, structured output.

MCP ingestion means an agent can discover an MCP tool, figure out the right params, and save it as a button so it never has to rediscover it. Just press the button next time.

Source lives in `apps/buttons/`. The CLI is an independent binary that stores data in `~/.buttons/`.

Parent issue: #251. Sub-issues track each phase.

## Tech Stack

- **Language:** Go 1.26+
- **CLI framework:** Cobra (`cmd/` at root level, not internal)
- **TUI:** Bubble Tea + Lip Gloss (Phase 3)
- **Forms:** Huh (Phase 2)
- **Storage:** JSON files for everything — button/drawer specs in `~/.buttons/buttons/<name>/button.json`, run history as timestamped JSON files in `~/.buttons/buttons/<name>/pressed/`
- **Build:** `go build` or `make build`

## Project Structure

```
cmd/           — Cobra commands (public, thin wrappers that delegate to internal/)
internal/
  button/      — Button spec entity, CRUD service, JSON file store
  drawer/      — Drawer spec entity, CRUD service, JSON file store
  engine/      — Execution engine (os/exec, timeouts, process group kill)
  history/     — Run history (JSON files under each button's pressed/ directory)
  config/      — Paths (~/.buttons/), non-TTY detection, JSON output helpers
  tui/         — Bubble Tea board (Phase 3)
```

## Critical Conventions

### JSON Output Contract
Every command supports `--json`. Non-TTY auto-detects and outputs JSON. Use `config.WriteJSON()` and `config.WriteJSONError()` — never raw `fmt.Println` with JSON strings.

```json
{"ok": true, "data": {...}}
{"ok": false, "error": {"code": "NOT_FOUND", "message": "..."}}
```

Error codes are uppercase snake_case: `NOT_FOUND`, `TIMEOUT`, `SCRIPT_ERROR`, `MISSING_ARG`, `RUNTIME_MISSING`, `VALIDATION_ERROR`.

### Button Spec Schema
Every JSON spec file must include `"schema_version": 1`. This is non-negotiable for future migration support.

### Drawer Spec Schema
Drawers are workflow chains of buttons. Spec stored at `~/.buttons/drawers/<name>/drawer.json` with `"schema_version": 1`. The canonical JSON Schema lives at `docs/schemas/drawer.schema.json` and is generated from the Go struct via `go generate ./...`. Agent-facing CLI verbs: `create`, `add`, `connect`, `press`, `list`, `remove`, `summary`. See `docs/examples/apify-to-snowflake.md` for a full walkthrough.

References between steps use `${step_id.output.field}` (dotted-path) or `$ENV{VAR_NAME}` for secrets. Stage 2 swaps the dotted-path resolver for CEL while keeping the `${...}` wire format stable.

### `buttons summary` and `--summary`
Workspace introspection: `buttons summary [--json]` dumps buttons, drawers, recent runs, and failures in one response so agents orient themselves in a single tool call. Bare `buttons` invokes the same.

`--summary` is a universal flag: applied to any mutating command (`press`, `drawer press`, `drawer add`, `drawer connect`, etc.) it returns a read-only plan instead of executing. Never mutates, never touches the network, never side-effects.

### Execution Security
- `context.WithTimeout` on every `os/exec` call, default 60s
- Kill process GROUP not just process (`Setpgid` + `Kill(-pid, SIGKILL)`) after 5s SIGTERM grace
- Args passed as env vars (`BUTTONS_ARG_<NAME>`), never interpolated into shell body
- File permissions: `0700` on data directories and code files, `0600` on spec/history JSON

### MCP Server (Phase 2)
- `mcp_enabled: false` default on every button — explicit opt-in required
- Meta-tool pattern: 3-5 tools (buttons_list, buttons_press, buttons_inspect), not 1:1 button:tool
- Rate limits: 10 calls/min default, 1 concurrent per button
- Hard timeout cap: 120s for MCP-triggered executions

### Cloudflare Workers (Phase 4)
- JS/TS buttons ONLY can deploy to Workers. Shell buttons cannot.
- Fail loudly at deploy time if runtime is incompatible.

## Phase Order (Revised)

1. Core CLI: create, press, list, history, rm + `--json` everywhere
2. Drawers + Batteries + **MCP server** (pulled forward — killer feature)
3. Parallel (smash) + TUI Board + Scratchpad
4. Deploy (CF Workers) + REST API + Triggers
5. Store + Distribution

## Running

```bash
go build -o buttons .    # build
./buttons --help         # verify
make build               # build with version injection
```


## External Documentation References

When building features or answering questions related to the CLI framework, TUI, or CLI UX, consult the official sources as the source of truth:

- **Cobra CLI** — https://cobra.dev/docs/
- **Bubble Tea** — https://pkg.go.dev/charm.land/bubbletea/v2
- **CLI Design Guidelines** — https://clig.dev/
