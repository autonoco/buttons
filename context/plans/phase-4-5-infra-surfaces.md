# Phase 4/5 — remaining infra-blocked surfaces

Status map + design for the Buttons Phase 4/5 sub-issues that are **not shippable as standalone
working code yet** because they depend on external infrastructure (a Cloudflare account, the
registry server, a hosting target). Everything buildable-without-infra has already shipped — this
doc is the design + the "what unblocks it" for the rest, so a reviewer can green-light the infra
and the code lands behind interfaces that already exist.

Companion to `context/plans/buttons-as-skill-runtime.md` (distribution model) and
`context/plans/buttons-ui/` (web UI).

## What already shipped (for context)

| Issue | Feature | PR | Notes |
|---|---|---|---|
| #257/#186 | button version/tags/provenance schema | merged | `content_hash`, `version`, `tags`, `requires`, `requires_batteries`, `source` |
| #274 (install) | `buttons install <name\|tag:x>` from a `Source` | #190 | `internal/store` — the `Source` interface everything below codes against |
| #274 (update) | `buttons update` — binary + installed content | #192 | drift via `content_hash` |
| #276 (client) | `buttons publish` to a `Source` | #198 | publish→install round-trips against a local source today |
| #267 (smash) | `buttons smash a,b,c` | #191 | parallel button exec |
| #267 (drawer) | `buttons drawer NAME press --mode parallel` | #197 | dependency-aware wave executor |
| #270 | `buttons serve` REST API | #193 | the HTTP host the streaming + webhook surfaces mount on |
| #272 | `buttons trigger` cron/watch/webhook | #195 | webhook routes mount on `serve` |
| #265 | `buttons mcp` stdio server | #194 | meta-tools, rate-limit, 120s cap |
| #277 | `buttons import` skill/code/url | #196 | `mcp` import adapter deferred (needs an MCP client) |

The **seam** that makes the rest cheap: install/publish/update all code against the `Source` /
`Publisher` interfaces in `internal/store`, and serve/triggers/mcp all press through
`internal/runner`. New transports (registry HTTP, CF Worker, WebSocket) are new *implementations*
of seams that already exist and are tested.

---

## #271 — `buttons deploy`: button → Cloudflare Worker

**Goal.** `buttons deploy <name>` runs a button as an always-on HTTP endpoint on Cloudflare Workers
(the hosted analog of `buttons serve`, which already exposes the same press path in-process).

**Design.**
1. **Generate** a Worker bundle from the button spec: a `worker.js` that, on `fetch`, validates args
   against the button's `args` schema and invokes the button. For **HTTP/API buttons** this is a
   direct fetch proxy (the Worker *is* the button — no runtime needed). For **code buttons**, the
   Worker shells to a container/queue (Workers can't run arbitrary shell) — so v1 deploy targets
   **HTTP buttons + prompt buttons**; code buttons are explicitly out of scope until a container
   target exists.
2. **`wrangler.toml`** generated from `--name`, routes, and the `requires_batteries` (mapped to
   Worker secrets — names only, never values, matching the battery model).
3. **Deploy** = shell out to `wrangler deploy` (already the team's CF tool), or call the CF API with
   an account token.

**Buildable now (no infra):** `buttons deploy <name> --dry-run` that emits `worker.js` +
`wrangler.toml` to stdout / a dir. This is testable and lets a reviewer see the artifact before any
account exists. **Recommend landing the `--dry-run` generator first** as a standalone PR; the actual
`wrangler deploy` shell-out is a 20-line follow-up behind a CF account.

**Infra blocker:** a Cloudflare account + `wrangler` auth + a Workers/R2 plan. Secret provisioning
(batteries → Worker secrets) needs the account's secret store.

**Maps to the control-plane model:** the Worker is a *runtime-adapter*; deploy is *mount-time*;
batteries→secrets is the *secret-binding* (requirement names travel, values are provisioned at the
edge).

---

## #273 — real-time press streaming (WebSocket)

**Goal.** Stream a press's stdout/stderr/progress to a client as it runs, over the `serve` listener.

**What already exists.** `engine.Execute` takes a `LineSink` (a `chan LogLine`) and tees child
stdout/stderr line-by-line; buttons also append structured events to `$BUTTONS_PROGRESS_PATH`
(JSONL). `buttons press --follow` already consumes the sink locally. So the **producer side is
done** — streaming is purely a transport over `serve`.

**Design.**
- Add `GET /api/buttons/{name}/stream` to `internal/apiserver`. Two transport options:
  - **SSE (recommended v1):** `text/event-stream`, zero new dependencies, works through proxies, and
    maps perfectly to a one-way log stream. Each `LogLine` → one `data:` event; a final `event: result`
    carries the `engine.Result`.
  - **WebSocket (the issue's literal ask):** needs a ws library (`coder/websocket` or
    `gorilla/websocket`) and is bidirectional (overkill for a log stream, but matches the title).
- Either way: spawn the press via `internal/runner` with a sink wired to the HTTP stream; bearer auth
  + the per-button rate/concurrency guards from the MCP work apply.

**Buildable seam:** `serve` (#193) + the engine sink already exist; this is ~80 lines on top. The
only reason it's in this doc rather than shipped: it depends on **#193 merging first** (it edits
`internal/apiserver`), and a deliberate **SSE-vs-WebSocket** call from the reviewer. **Recommend SSE**
unless a bidirectional control channel is actually needed.

**Infra blocker:** none beyond #193 — this is the most readily unblocked item. Could ship the day
#193 lands.

---

## #278 — distribution surfaces: SSH board · Web UI · Teams

Three sub-surfaces, each infra-bound:

**SSH board** — `buttons board` rendered over SSH so a team hits one host to press shared buttons.
- *Design:* wrap the existing bubbletea board (`internal/tui`) in a Wish (`charmbracelet/wish`) SSH
  server; auth via SSH public keys; each session gets a board bound to a shared (or per-user) buttons
  dir. Presses go through `internal/runner`.
- *Infra blocker:* a host to run the SSH daemon, key management, and a decision on shared-vs-per-user
  state. *Buildable seam:* the TUI + runner exist; this is a new front-end host.

**Web UI** — already designed in `context/plans/buttons-ui/`. Served by the **registry Worker**
(same origin as the registry API). Lists/searches buttons, shows specs + run history, triggers
presses via the REST API (#193).
- *Infra blocker:* the registry server (#275) + hosting. *Seam:* the REST contract (#193) is the API
  the UI consumes — already shipped and stable.

**Teams** — multi-user/multi-tenant: per-user registry keys with scopes (`read`/`publish`),
revocation, and team-shared button collections (by `tag`).
- *Design:* lives in the registry's D1 (per `autonoco/buttons-registry` PLAN.md Phase 2); the CLI
  side is just which bearer key the `HTTPSource` sends.
- *Infra blocker:* the registry server + D1 + an identity model. *Seam:* `Source`/`Publisher`
  already abstract the transport; teams is an auth layer on the registry impl, not a CLI change.

---

## Recommended unblock order

1. **#273 via SSE** — ship the moment #193 merges; no external infra, immediate value.
2. **#271 `--dry-run` generator** — ship the Worker/wrangler artifact generator now (testable);
   wire `wrangler deploy` once a CF account exists.
3. **Registry server (#275)** — unblocks #276 (registry transport), #278 Web UI, and #278 Teams in
   one move. Biggest leverage.
4. **#278 SSH board** — independent; needs only a host + key policy.

Every item above lands as a new *implementation* of an interface that already exists and is tested,
so the risk is in the infra + product calls, not the code.
