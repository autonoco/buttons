# Buttons Flow — PRD

**Status:** Draft for build
**Owner:** Bob
**Date:** 2026-07-31
**Companion:** `buttons-flow-agent-prompt.md` (the prompt to hand a coding agent with this PRD)

---

## 1. Summary

Buttons Flow is the runtime for the flow drawers this repo already ships. `drawer_kind: "flow"` exists today — authorable, validated, content-hashed, and distributable through the registry — and pressing one returns:

> `FLOW_RUNTIME_REQUIRED: flow drawer %q is activated and run by a Buttons Flow runtime; it cannot be pressed as an action drawer` (`cmd/drawer.go:475-480`)

This PRD specifies that runtime. It is **not a new definition format, not a new binary, and not a rewrite of the drawer engine.** It is `cmd/flow.go` + `internal/flow/`: a durable engine that turns a FlowDefinition into a live project board over a task store, carrying over — mechanism by mechanism — what Turtleflow proved in production.

**One sentence:** `buttons flow` makes the definition format we already published into what it describes — a durable, always-on project board worked by agents.

**A board, not a workflow (normative):** an action drawer is a workflow — you press it, it executes, it ends. A flow is a **durable project board** — a place where tasks live and agents work them continuously. Nobody runs a board. `init` installs everything that keeps it live; from then on the user's verbs are about tasks and oversight, never execution.

**Vocabulary rule (normative):** engine words — tick, journal, lease, activation, backend, ingress, pump, sweep, attached/detached, tenant, delivery, revision, holder, TTL, serve, run loop — live in this document and the code. They never appear in CLI verbs, flags, help text, or human-readable output. The CLI speaks plain words — **init, task, status, logs, approve**, with `reject`, `cancel`, `rm`, and the machine verb `sync` — and the rule that matters is not the count: every verb is a word a teammate would use, every optional decision is a default. There is no start, stop, or run; no retry (if a human has to retry, the system failed — recovery is conversation, §7.8); and no pause (teams don't pause — you tell the worker and you take the task: a comment plus a claim, §7.3).

## 2. Background

### 2.1 What Buttons already has (verified against code, not the README)

- **The flow definition format** (`internal/drawer/entity.go:93-153`): `FlowDefinition{InitialStage, Manager{Agent, SystemPrompt, HeartbeatSeconds}, Limits{MaxActiveTasks, MaxRuntimeSeconds, MaxAttemptsPerStage}, Stages[]{ID, Title, SystemPrompt, Manager, Worker, SessionPolicy, Triggers(heartbeat|cron|event|webhook), Transitions, Completion, Retry, TimeoutSeconds, Concurrency}}`. Authoring (`buttons drawer create X --kind flow`, `stage add`, `set flow.*`), validation (`validateFlowDefinition`, `internal/drawer/validate.go:334-455`), normalization + content hashing (`flow_normalize.go`), registry publish/install with byte-verified `flow-definition.json`. Agent refs are deliberately deferred to activation: `activation.manager` | `activation.worker` | `agent:<tenant>:<name>` (that vocabulary is the shipped format's — it stays internal).
- **Action drawers**: implemented step kinds `button`, sub-`drawer`, `for_each`, `switch` (CEL), `aggregate`, `wait`; retries with backoff; `on_error`; full CEL `${step.output.field}` data flow; parallel wave executor.
- **A registry** whose packages can contain drawers, with transitive `requires`.
- **Daemonless cross-process coordination precedent**: `internal/queue/queue.go` flock slots.
- **JSON CLI contract** (`{"ok":…}` envelope), `pressed/` history, an integration-test harness that builds the real binary against an isolated `BUTTONS_HOME`.

### 2.2 What Buttons entirely lacks (all new construction)

No durability: a drawer run is an in-memory loop; history is written once, at the end (`executor.go` `finalize`) — a killed run leaves **no record**. No resumability, no leases or claims of any kind, no daemon (cron/watch triggers die with `buttons serve`), no durable timers (`wait` is in-process `time.After`), no human-in-the-loop signals, no run cancellation, no cross-run concurrency control, no flow runtime.

### 2.3 What Turtleflow proved

Turtleflow (`autonoco/turtleflow`) runs agentic boards on GitHub Projects in production: org-level `Pipeline` issue field as the state machine (board as mirror), exe VMs as credential-free worker environments, one custom GitHub App, a durable webhook ingress, heartbeat supervision, and an operator surface. Notably, Turtleflow's CLI is `init` and operator commands — it has no run verb, because a board is not something you run. Its durability mechanics were extracted as **35 code-verified invariants** (Appendix A). This PRD is the port: the portable core carries over unchanged; the GitHub-coupled parts become one adapter behind a generic contract; three known gaps (no reconciliation poll, no TTL'd lease, O(n) event-id dedup) get closed rather than inherited.

### 2.4 The Jul 31 design decisions

From the standup (Bob + Eric): the orchestrator must not care where task data lives — stable commands map to swappable fulfillment, the way `package.json` maps script names. **Task is the only noun.** Props (single-valued; flows branch and trigger on them) versus tags (multi-valued open labels). Templates and the registry are the opinionated layer; well-performing flows get saved and shared. Build it *in* Buttons — each mapping is a button-shaped concern, and it revives the flow drawer surface.

Decisions locked with Bob:
1. `buttons flow` **is** the missing runtime — no new definition format.
2. Native Go in this repo (`internal/flow/` + `cmd/flow.go`); Turtleflow is the invariant reference, never a dependency.
3. v1 ships **both** stores: local (file) and GitHub, behind one contract.
4. GitHub App identity v1: **BYO App per org** via the manifest flow; the hosted shared-gateway model is a later product tier.
5. Liveness is installed by `init`, never invoked by the user: locally a user-level service; on GitHub the runner lives on an exe.dev VM installed by `init --on github`, with a generated Actions-cron workflow (invoking the machine verb `sync`) as the serverless fallback.
6. `init` is the setup verb — matching `buttons init` and `turtleflow init`, which is the house vocabulary for "do the installs."

## 3. Goals

1. A published flow drawer becomes a live, durable board with one command: `buttons flow init <drawer>`. After init there is nothing to run — tasks added by anyone, from anywhere, are worked continuously.
2. An agent or human manages tasks with Bob's verb set — `add, list, search, read, update, rm` (+ `claim`, `comment`), filtering with `--filter k=v` — and never thinks about where the data lives.
3. Kill any process at any moment and lose nothing: every accepted event runs to completion or is pinned as a failure exactly once, in place.
4. The same flow drawer runs unmodified on the local store and on GitHub.
5. Provisioned == working: `init` reports success only after a real task has moved through the real machinery (opt out with `--no-verify`).
6. The surface is plain words (init, task, status, logs, approve; reject, cancel, rm; machine verb sync); everything else is engine-internal. No retry (recovery is conversation) and no pause (taking over = commenting + claiming, like any teammate).

## 4. Non-goals (v1)

- **No new definition format.** Schema v3 is one coherent, versioned extension set — the role registry, gates, capabilities, stage prompts, and board config (§10); everything else is the shipped format.
- **No permission model and no sandbox settings.** Agents always run full-access ("yolo") — exactly what Turtleflow's boxes already run (`danger-full-access`, approval never). The safety boundary is not permissions; it is the publish path (workers never push; the trusted publisher enforces protected paths and secret patterns) and the bounded-decision validation.
- **No event bus.** The journal + reconciliation is the delivery mechanism.
- **No UI.** `flow status` / `flow logs` JSON + the GitHub board are the surface.
- **No multi-store per board.** One store per board.
- **No hosted shared gateway** (per-repo child processes, double HMAC, Agent-Turtle-style multi-tenancy) — returns as the Autono product tier.
- **Cut from the Turtleflow port**: Bun migration, first-ticket creation, the 27-prompt/16-skill template tree (FlowDefinition stage prompts + registry `requires` replace it), CodeRabbit integration, preview supervision, issue-dependency fan-out (a `deps` prop is reserved).
- **Deferred to v1.1**: standalone `plan`/`apply` verbs (the engine exists; `init` and `status` are its v1 faces), moving a board between homes (local → exe), a `ButtonStore` backend (custom store = five conventional buttons), a local `sqlite` store + gitcrawl-fed cached reads, promoting `buttons task` to a top-level alias.

## 5. Core concepts

| Concept | Definition | Implemented as |
|---|---|---|
| **Task** | The only noun. Work with props and tags. | `task.Task` (§6.1) |
| **Comment** | The human↔agent channel on a task; every comment is a turn. | GitHub: issue comments; local: `tasks/<id>.conversation.jsonl`, rendered by `task read` |
| **Property** | Single-valued key/value; flows branch/trigger on it. `status` mandatory. | `Props map[string]string` |
| **Tag** | Multi-valued open label. | `Tags []string` |
| **Store** | Where tasks live (local or GitHub); an implementation of the contract. | Go interface composition (§6) — internally "backend" |
| **Board** | The durable, always-on project board a flow drawer describes; live from `init` onward. | internally an "activation": `activation.json`, content-hash pinned |
| **Runner** | The always-on process that keeps a board live. Installed by `init`, never invoked by the user. | user service (local) / exe VM (GitHub) / Actions cron (fallback) |
| **Flow drawer** | The definition of a board. Already shipped. | `drawer_kind:"flow"` |
| **Template** | A packaged board definition + its requirements. | Registry package (exists today) |

**Props vs tags is load-bearing** (authoring concern, not a user-surface concern): `status` must be a prop because "status changed from X to Y" is only a triggerable event if status holds exactly one value. Rule: if a flow branches on it or triggers from it, it is a prop; otherwise a tag.

Reserved namespaces:
- Props `flow.*` (runtime-owned, internal): `flow.alert`, `flow.attempts.<stage>`, `flow.stage_entered_at`, `flow.pending_approval`, `flow.claimed_by` (read model only — the durable lease is authoritative). The **one user-visible signal** is `needs_attention`, always with a reason from a fixed vocabulary (ported from Turtleflow's health findings): `stalled-worker`, `failed-worker`, `missing-environment`, `disk-pressure`, `approval-pending`, `escalated`, `unknown-worker`, `recovery-failed`, `no-agent-configured`. The internal props feed it.
- Tag `flow:keep` (retain sessions/workspaces on cleanup) — the one sticky operator flag. Turtleflow's takeover label has no successor: taking over a task is a comment ("I've got this") plus a claim, not a state (§7.3).
- User-settable per-task props the engine honors: `scope` (multi-app repos: which app a task belongs to — selects the template's per-app setup/verify; Turtleflow's `scope:` labels generalized), `worker` (route this task's turns to a named role or agent — "this commission goes to the senior-design agent"; unknown name → `needs_attention: unknown-worker`), `skills` and `tools` (additive, §10.4). Prop `deps` reserved for v2 task dependencies.

### 5.1 Boards are authored, not hardcoded

Any combination of stages is just a flow drawer: `buttons drawer create X --kind flow`, `stage add ID --title T [--system-prompt P]`, `set flow.*` — all shipped today, scriptable, and agent-drivable (the CLI plus MCP `buttons_create`). An agent that wants a five-stage research board writes one and inits it; nothing in the runtime knows any particular stage set. Registry templates (swe, marketing, research) are the opinionated layer on top; full flexibility underneath is the point. The team is authored too: a named role registry (each role with its own prompt, model, heartbeat, escalation — §10.2) that stages reference, so "reviewer" is defined once and used everywhere. Capabilities — system prompts, skills, tools — attach per node (board, stage, task) and inherit downward; see §10.

## 6. The store contract

This section is normative. Implementing it *is* a Buttons Flow store. (Interface names below are internal Go vocabulary — none of it surfaces on the CLI.)

```go
type Backend interface {
    task.Store       // §6.1
    task.WorkLog     // §6.2
    task.EventSource // §6.3
    task.Authorizer  // §6.4
}
// Optional capability, discovered by type assertion:
//   lease.Store    // §7.3 — store-anchored leases; both v1 stores omit it
```

### 6.1 task.Store

```go
type Task struct {
    ID        string            // local: "t-<ulid>"; github: issue number (repo-scoped)
    Title     string
    Body      string
    Props     map[string]string // single-valued; "status" always present
    Tags      []string
    CreatedAt, UpdatedAt time.Time
    Revision  int64             // optimistic concurrency; github: derived. Internal — never in human output.
}

type Query struct {
    Props        map[string]string // equality, AND only
    Tags         []string          // must contain all
    UpdatedSince time.Time
    Limit        int
}

type Patch struct { // merge-not-replace, always
    Title, Body *string
    SetProps    map[string]string
    UnsetProps  []string
    AddTags, RemoveTags []string
}

type UpdateOpts struct {
    ExpectRevision int64        // 0 = skip (human CLI); the engine always sets it
    Lease          *lease.Lease // fencing guard
}

type Store interface {
    Add(ctx, Task) (*Task, error)
    Get(ctx, id string) (*Task, error)
    List(ctx, Query) ([]*Task, error)
    Search(ctx, text string, q Query) ([]*Task, error)
    Update(ctx, id string, p Patch, o UpdateOpts) (*Task, error)
    Remove(ctx, id string) error
}
```

Rules: unknown props and tags round-trip untouched; update merges, never replaces; `claim` is **not** a Store method — it composes `lease.Acquire("task:<id>")` + a `flow.claimed_by` read-model write, so stores stay dumb and mutual exclusion stays with the runtime. Query syntax stays deliberately trivial (comma-separated `key=value`, AND only; `tag=<t>` matches tag membership — the one query path for props and tags alike; no OR, no negation, no ranges; do not extend it).

### 6.2 task.WorkLog — the portable run summary

The local `runs/` journal is runtime truth; the WorkLog is the bounded summary that travels **with the task** — Turtleflow's base64url work-state blob generalized. Bounds live in the contract, not the adapter: merge by `Key = sha256(eventID)` (idempotent append), retain newest **20 runs / 14 days**, parse failure reads as empty, never as an error (invariants 14–17).

```go
type WorkLogEntry struct {
    Key           string    // sha256(eventID)
    Stage, Role, Result, Summary string
    DurationMs    int64
    TranscriptRef string
    RecordedAt    time.Time
}
type WorkLog interface {
    AppendRun(ctx, taskID string, e WorkLogEntry) error
    ReadRuns(ctx, taskID string) ([]WorkLogEntry, error)
}
```

GitHub: merged by PATCH onto one pinned bot comment. Local: `tasks/<id>.worklog.json` sidecar (appends must not churn task revisions).

### 6.3 task.EventSource — dual event IDs are contract-level

```go
type EventSource interface {
    Pull(ctx, cursor string) ([]NormalizedFlowEvent, string, error)
}
```

Every event MUST carry both a **DeliveryID** (transport-unique per firing) and an **EventID** (logical identity, stable across transports — e.g. `task:<id>:<revision>`). This makes Turtleflow's dual idempotency (`github-webhook.ts:363-366`) an obligation on every store: a webhook firing and a poll observation of the same change dedupe to one dispatch. (Entirely internal — users never see either ID.)

Event-ID derivation rules (normative): heartbeat `hb:<flow>:<stage>:<floor(unix/every)>`; cron `cron:<flow>:<stage>:<scheduledRFC3339>`; task change `task:<id>:<revision>` — **including changes observed by reconcile**, which reuse the same logical ID so a webhook firing and a poll observation of one change dedupe through the index (the DeliveryID stays synthetic per observation); webhook = transport delivery header, else `wh:<hookID>:<sha256(body)[:16]>`; operator `op:<uuid>` (never deduped). The rest of the catalog: stage change `stage:<id>:<revision>` — **a `status` change emits `stage-changed` only, never a second `task-updated`: one change, one event**; comment `comment:<task>:<comment-id>` (local: the conversation line number); close/reopen `closed:<task>:<revision>` / `reopened:<task>:<revision>`; run lifecycle `run:<runID>:<state>`; health `health:<task|board>:<finding>:<floor(unix/interval)>` (bucketed so a flapping check cannot storm); GitHub extras content-derived under the same obligation — `ci:<task>:<check-run-id>`, `pr:<task>:<pr-number>:<action>`.

**The event catalog (engine-defined; emission is not configurable — everything that happens is emitted). Routing of `task-*`, `stage-changed`, and `operator` events is automatic, per the table; `triggers` exist to *add* hearing — schedules (heartbeat/cron), webhooks, and the explicit `run-*`/store-specific subscriptions:**

| Kind | Emitted when | Who hears it |
|---|---|---|
| `task-added` | a task is created, through any door (CLI, GitHub UI, an agent) | the initial stage |
| `task-updated` | props/tags/title/body change (revision-keyed) | the task's current stage |
| `task-commented` | anyone comments — every comment is a turn | the current stage (this is how feedback, answers, and recovery arrive) |
| `stage-changed` | `status` moved — a validated advance, or an external move observed by reconcile | the target stage |
| `heartbeat` / `cron` | a stage trigger's schedule fires (bucketed IDs, §7.1) | the stage that declared the trigger |
| `webhook` | an external system posts to a declared hook | the stage that declared the hook |
| `run-*` lifecycle | a turn starts / retries / completes / fails | only stages that explicitly subscribe — echo-suppressed (§7.7) |
| `task-closed` / `task-reopened` | a task is closed or reopened, through any door | the current stage; cleanup |
| `health-changed` | a worker/environment health finding changes (the `needs_attention` vocabulary) | roles that `watch` it — this is how platform self-healing composes with delegation (§7.6) |
| `operator` | approve / reject / claim / cancel | the engine itself, priority 100 |

Store-specific kinds: the GitHub store additionally emits `ci-completed`, `pr-changed`, and `pr-reviewed`, correlated to their task by parsing the task's derived branch (§9.1) — the swe template's review loop subscribes to these; the local store never emits them.

Subscription is declarative and lives in the drawer — `FlowStage.Triggers[]{kind: heartbeat|cron|event|webhook, every_seconds, cron, event_type, hook_id}` (shipped format, `entity.go`). There is no runtime "subscribe" API and no emission config in v1: an agent changes what it hears by editing the definition (authoring is scriptable), which lands as a hash-gated `init --upgrade`.

### 6.4 task.Authorizer

```go
type Authorizer interface {
    AuthorizeHuman(ctx, actor string, e *NormalizedFlowEvent) (bool, error)
}
```

Gates operator/feedback events **before** the journal claims them — unauthorized means ignored, never claimed (the Turtleflow ingress path). GitHub: collaborator permission ∈ {admin, maintain, write}. Local: the OS user, optional allowlist. Gate approvals (§10) use the same interface.

### 6.5 Stores: Go interfaces, buttons as the extension mechanism

Weighed honestly: task-* buttons as the store (each verb a pressed button) gives free JSON contract + history and dogfoods Buttons — but the hot path does tens of store calls per scheduler pass, CAS/fencing can't cross a shell boundary, and the durability invariants would rest on user-editable scripts. **Decision: Go interfaces are the contract; v1 ships the local and GitHub stores native. `ButtonStore` (v1.1) maps each verb to conventionally named buttons (`task-list`, `task-add`, …) with a published JSON contract — a custom store is then five buttons.** Agent turns go through the agent runtime (§9.1) — sessions, reattach, transcripts; buttons remain the tool surface agents press *inside* their turns (the capabilities `tools` list, §10.4).

## 7. The runtime

### 7.1 Liveness is installed, not invoked

Internally, the unit of convergence is a **tick**: read durable state, do bounded work, journal, stop. The **runner** is a loop over ticks plus the webhook mount, and it is infrastructure `init` installs, not a command a user types:

- **Local board**: a user-level service (launchd agent on macOS, systemd user unit on Linux), installed by `init`, restarted by the OS, surviving reboots. The board is as live as the machine — a sleeping laptop's board catches up on wake (level-triggered timers + reconciliation absorb the gap; late, never wrong).
- **GitHub board**: the runner on an exe.dev VM, installed by `init --on github` (the `gateway-deploy.ts` pattern: uploaded bundle, systemd unit with `ProtectSystem=strict`, health gate on deployment SHA). A generated Actions-cron workflow invoking the machine verb `sync` is the serverless fallback (1–5 min latency vs the VM's ≤10s).
- **The machine verb**: `buttons flow sync` advances the board one step and exits — what the service units, Actions workflows, and CI invoke. Documented, not hidden: agents read cron files and help alike, so its help says the one honest thing — "normally the runner calls this for you." Concurrent tickers are safe by construction (bucketed event IDs + lease claims), so the service, a cron pass, and a stray manual invocation coexist.

**No logic may exist in the runner that is not in a single tick.** The step budget per tick is derived from the invocation context, not a flag.

Tick anatomy (each phase bounded; journal-before-effect):
1. **Converge setup** — the runner refuses to work a board whose provisioning is incomplete; `init` is the user-facing converge (consent-gated where it creates real resources, resumable across the PR-merge gate, idempotent — re-running `init` resumes interrupted setup, repairs drift, and `--upgrade` adopts an edited definition). Definition gate: the drawer's normalized content hash must match the pinned hash (`FLOW_CHANGED` refusal: "this board's definition was edited — `buttons flow init NAME --upgrade` to adopt the edit").
2. **Reconcile** — `EventSource.Pull(cursor)` + diff observed state vs journal; external changes become synthetic revision-keyed events. Closes Turtleflow gap #1 (nothing recovered a webhook that never arrived).
3. **Fire due** — bucketed heartbeats/crons and durable timers (retry backoff, stage timeout, `MaxRuntimeSeconds`). Catch-up is level-triggered: after downtime, only the *latest* missed bucket fires per heartbeat/cron; durable timers all fire (edge-triggered obligations).
4. **Pump** — admit events through echo suppression → per-task serialization + caps + priority tiers → claim via TTL lease → run the turn (in-process, or launched on a remote worker and collected by later ticks).
5. **Sweep** — expired leases → `step.abandoned` → charged attempt → retry timer per `FlowRetry`, or exhaustion → `stage.exhausted`: task **pinned in place** with `needs_attention` **and a comment on the task saying what failed and what is needed** (port of Turtleflow's recovery comments) — never auto-advanced. Prune per retention bounds (automatic — there is no user-facing cleanup verb).
6. **Report** — JSON `{ok, reconciled, fired, claimed, completed, swept, blocked, next_due}`; `next_due` lets external schedulers pick their wake.

### 7.2 The journal (DeliveryStore port + the missing index)

Direct Go port of Turtleflow's `DeliveryStore` contract (`github-webhook.ts:55-68`) with an event-id index it lacked:

- `journal/events/<sha256(deliveryID)>.json` — **O_EXCL create IS the claim**; all rewrites tmp+rename, 0600.
- `journal/events/index/<sha256(eventID)>` — also O_EXCL-created; the index create is the logical-dedup barrier (fixes the O(n) scan, gap #3). Crash between index and record creation heals in `ReclaimRecoverable`.
- Bounded retries (3, backoff `min(2s, attempt*250ms)`) → **pinned as failed exactly once** even if reporting crashes (`DeadLetterReportPending` durable marker; restart re-reports, never re-routes). User surface: `flow logs --failed` lists them, and the failure is posted to the task's conversation; a human's reply (`task comment`) or the supervisor's own bounded decision re-dispatches it — there is no retry verb, internal replay resolves the delivery record itself, and delivery IDs never cross the human CLI (`--json` carries them for machines).
- Restart replays everything non-completed. Retention `Prune(14d, 2000)`, `processing` exempt; results bounded at 128 KiB (overflow stored as `{stored:false, sha256}`).

### 7.3 Leases — TTL + fencing (the thing Turtleflow never had; entirely internal)

Turtleflow's mutual exclusion was in-process, safe only under one-gateway-per-repo. Buttons Flow assumes multiple workers eventually:

```go
type Lease struct { Key, Holder string; Token uint64; AcquiredAt, RenewedAt, ExpiresAt time.Time }
type Store interface {
    Acquire(key, holder string, ttl time.Duration) (*Lease, error) // idempotent same-holder (extends, same token)
    Renew(l *Lease, ttl time.Duration) (*Lease, error)             // fails if token superseded
    Release(l *Lease) error                                        // no-op if superseded
    Get(key string) (*Lease, error)
}
```

Mandatory semantics: **expired == unleased** (taken with `Token+1`); **CONFLICT on lost race** (user-facing message: "already claimed by X until <time>"); **idempotent re-claim** by the same holder; **fencing** — guarded writes verify the presented token is current, so a resurrected stale worker gets CONFLICT ("task changed since you read it — re-read and retry"), not silent corruption. Local impl: O_EXCL create fast path; contention paths RMW under flock (the `internal/queue` idiom). `lease.Store` sits in the contract as an optional capability — both v1 stores omit it and the runtime uses its local file store; store-anchored leases (GitHub verify-after-write) land in v2 with zero interface change, unlocking serverless multi-runner boards.

**Humans claim like workers do.** `task claim` by a human takes the same lease an agent turn takes; per-task serialization (invariant 7) then keeps agent turns off the task with no extra mechanism — no pause state, no takeover tag. The claim's expiry hands the task back automatically (crash-safe for humans too); events and comments accumulate normally in the meantime and are worked on hand-back. Announcing it is just a comment.

**Singleton board lease**: the runner acquires `singleton:<board>` (TTL 30s, renew 10s) + `runner.lock` flock — one-writer-per-board *enforced*, not topology-assumed (upgrades invariant 11). Honest boundary: file leases fence one host, and fencing protects board-state writes, not arbitrary side effects an agent performs mid-turn — the same boundary Turtleflow had.

### 7.4 Scheduler (CardWorkScheduler port)

1:1 port of Turtleflow's `scheduler.ts` (89 lines, zero platform coupling): per-key serialization (one task = one running turn), global cap = `FlowLimits.MaxActiveTasks` (default 5), priority tiers with FIFO within tier, `Depth()` for heartbeat collapse. Added: per-stage admission cap from `FlowStage.Concurrency`.

```go
const (
    PriorityOperator    = 100 // cancel/approve
    PriorityFeedback    = 80  // human comments/updates
    PriorityStageMove   = 60
    PriorityExternal    = 40  // webhook/event triggers
    PriorityMaintenance = 20  // heartbeat, cron
)
```

### 7.5 Run checkpoints (what the drawer executor lacks)

Flow runs checkpoint at every phase boundary: `runs/<runID>.json` atomically written at **dispatched** (before agent spawn — the ack-before-dispatch analog), **running** (pid + session), and terminal state; `runs/<runID>.log.jsonl` is an append-only transcript — a killed run still yields a readable record (invariant 33). Runner startup rewrites dead-pid `running` records to `interrupted`; the causing delivery is non-completed, so replay re-dispatches; `SessionPolicy` decides reattach vs fresh. **Failure never advances state**: `status` is written only on a validated advance verdict; failure paths set `needs_attention` (internally `flow.alert`) and never touch `status` (invariant 6).

### 7.6 Verdicts — bounded decisions, engine-bound evidence

A manager turn ends with a JSON verdict:

```json
{"decision": "advance | retry | hold | delegate | escalate",
 "task": "<task id — echoed>", "stage": "<current stage id — echoed>",
 "to_stage": "<stage id — advance only>",
 "to_role": "<role — delegate only>", "instruction": "<delegate only, ≤10k chars>",
 "summary": "<always>"}
```

Re-validated in the core (`internal/flow/decide`) against rules **computed from the FlowDefinition** — `task` and `stage` must echo the dispatched turn exactly; then per decision:

- **advance** — `to_stage` ∈ `FlowStage.Transitions`; withheld pending approval out of a gated stage (§10.1 — accepted and recorded, parked as `flow.pending_approval`, not rejected); refused outright for heartbeat-origin turns (invariant 23). The only decision that advances the task along the pipeline.
- **retry** — send the work back to the stage's worker for another attempt; charges an attempt toward the bounds.
- **hold** — deliberately do nothing (waiting on a human, on CI, on time); nothing changes, no attempt charged, the summary lands in the conversation.
- **delegate** — dispatch `to_role` (validated against the v3 registry) with `instruction`; non-advancing — Turtleflow's self-healing move (supervisor sees `health-changed`, delegates the platform role to repair it).
- **escalate** — move the task to the acting role's `Escalation.ToStage` now, without waiting for `MaxAttempts` exhaustion (the same move, manager-initiated, §10.2); a role with no escalation configured gets `hold` + `needs_attention: escalated` instead.

Two agent-honest rules replace ceremony:

- **No hash copying.** Turtleflow made the reviewer echo an `artifact_hash` to prove what it saw — asking an agent to transcribe 64 hex characters, which agents mangle. The engine composed that prompt and journaled the evidence bundle under the turn's event ID: it already knows exactly what the reviewer saw. Verdicts are **bound engine-side to the journaled evidence of the turn that produced them** (invariant 22 kept, mechanism improved); an advance is refused if the artifact changed after that turn was dispatched. Agents never copy hashes.
- **Contract correction before punishment.** A reply whose tail fails to parse or validate gets a bounded re-ask (2 — Turtleflow's contract-correction attempts) in the same session: "your reply must end with this JSON shape; here is what was wrong." Only after the re-asks fail does the turn journal `decision.rejected` and charge a failed attempt, keeping the raw output in the conversation.

### 7.7 Echo suppression

Every lifecycle event carries `Origin.Session`; admission refuses delivering an event to the session that emitted it, and lifecycle events are telemetry unless a stage trigger explicitly subscribes to the kind (port of `runLifecycleEcho`/`runLifecycleWakeAllowed` — invariant 19). Internal; never user-visible.

### 7.8 Recovery is conversation — there is no retry

Three layers, none of them a human command: (1) transient errors retry themselves, bounded, in the journal (§7.2); (2) the supervising heartbeat nudges stalls with bounded decisions (§7.6) — autonomously; (3) anything past that is pinned in place with `needs_attention` and a **comment on the task** naming what happened and what is needed. A human answers by commenting — and because a comment is a turn (invariant 10), the reply itself re-dispatches the work. If the agent needs something specific, it asks in the conversation; if a human ever has to "retry," the system failed.

### 7.9 State residency

```
<data>/flows/<name>/
  activation.json   # drawer + content hash pin, bindings, store cfg (internal name)
  tasks/            # local store only (+ .worklog.json and .conversation.jsonl sidecars)
  journal/events/   # + events/index/
  runs/             # <runID>.json + <runID>.log.jsonl
  leases/
  runner.lock
  cursor.json       # EventSource poll cursor
  audit/            # provisioning receipts JSONL
```

Retention: journal 14d/2000 (processing exempt); runs 30d/500 (unreported failures exempt); tasks never auto-pruned. Pruning runs automatically at runner startup and after each pass — no user-facing cleanup verb. Everything 0600/0700, tmp+rename.

## 8. CLI surface

**`cmd/root.go` trap (verified):** the NAME-first `logs` rewrite at `cmd/root.go:74-86` would turn `buttons flow logs` into `buttons logs flow` — `"flow"` must join that switch's denylist. Follow `cmd/drawer.go`'s NAME-first verb-dispatch pattern.

| Verb | Effect |
|---|---|
| `flow init DRAWER [--name N] [--on local\|github] [--manager REF] [--worker REF] [--upgrade] [--no-verify]` | **make the board live.** Validate + pin the definition; bind agents (template/config defaults — `--manager`/`--worker` are overrides only; the agent runtime is acpx, installed by init — pinned, same as the box bootstrap); provision the store — **always previewing first**: the plan is printed and confirmed before any real resource is created, so no separate dry-run exists (GitHub: converging plan/apply with receipts; App manifest flow inline when none is registered; setup PR as the resume point); **install the runner** (local: user service; GitHub: exe.dev VM + Actions-cron fallback); then **prove it** with a throwaway task before reporting success. Re-running `init` converges: resumes interrupted setup, repairs drift; `--upgrade` adopts an edited definition. `--on` defaults to `local`, inferred `github` when the template binds a repo. |
| `flow task add\|list\|search\|read\|update\|rm\|claim\|comment` | the daily interface — see below |
| `flow status [NAME]` | no name = every board, one line each; with a name: counts by status, active/queued turns, pending failures + approvals, last heartbeat, runner health — plus drift findings (definition edited? provisioning diverged? store unreachable?) each with the command that fixes it |
| `flow logs [NAME] [--task ID] [--turn ID] [--failed]` | turn records + transcripts; `--failed` = what gave up after retries |
| `flow cancel TASK` | stop the task's active turn; its status is untouched |
| `flow approve TASK` / `flow reject TASK` | commit / refuse a gated transition (§10). Rejection doesn't route — your comment says what's wrong and the manager decides where the task goes; humans explain, they don't dispatch |
| `flow rm NAME [--purge]` | take the board down: uninstall the runner, remove state; tasks kept unless `--purge` |

Machine verb (documented; rarely typed by hand): `flow sync` — advance the board one step, then exit; what the service units, Actions workflows, and CI invoke. Its help says: normally the runner calls this for you.

Task verbs (Bob's vocabulary; filtering is `--filter`, one query path). Task verbs before `init` answer with the init command that makes the board:

```
buttons flow task add TITLE [--body B] [--prop k=v ...] [--tag t ...]
buttons flow task list [--filter k=v,...] [--limit N]
buttons flow task search "TEXT" [--filter k=v,...]
buttons flow task read ID
buttons flow task update ID [--prop k=v] [--unset-prop k] [--tag +t] [--tag -t] [--title T] [--body B]
buttons flow task rm ID
buttons flow task claim ID     # take the task yourself; agents stay off it until it hands back
buttons flow task comment ID "TEXT"   # talk to the agents on a task; a comment is a turn and queues behind a held task
```

All honor `--json` via the existing envelope. Error codes: `CONFLICT` ("already claimed by X until <time>"; the revision-mismatch variant is agent/`--json`-facing only — human CLI writes skip revision checks), `FLOW_NOT_FOUND`, `FLOW_CHANGED` (with the `init --upgrade` hint), `STORE_UNREACHABLE`. Machine details (revisions, holders, delivery IDs) appear only under `--json`.

**The comment door.** The operator surface also works where you already are — as comments: `/flow approve`, `/flow reject`, `/flow claim`, `/flow cancel` (Turtleflow's `/turtleflow …` pattern, ported). Parsing lives in the engine's comment-admission path, so it works identically on every store — a `task comment ID "/flow approve"` and a GitHub issue comment are the same event through the same Authorizer at the same priority. Forced environment teardown needs no command: remove `flow:keep` and close the task; the sweeper does the rest. On a GitHub board, the CLI's operator verbs and `task claim` are themselves delivered as `/flow` comments posted with your GitHub identity, executed by the runner — one path, two doors; the CLI waits for the runner's result reply before exiting, so outcomes (a claim's CONFLICT included) are synchronous either way. On a local board the CLI reaches the engine directly.

`buttons drawer NAME press` on a flow drawer keeps returning the existing `FLOW_RUNTIME_REQUIRED` code (compat), with a plain message: `"<name>" is a flow — make it a live board with: buttons flow init <name>`. Action drawers are untouched.

**Deliberately absent from v1** (each was in an earlier draft; cut as invented surface or workflow-thinking): `check` (status absorbs health + drift), `--dry-run` (init always previews and asks before creating anything — the preview IS the consent step), `reject --to` (humans explain, managers route), `--tag` (one query path: `--filter tag=x`), `retry` (recovery is conversation, §7.8 — transient errors retry themselves, the supervisor nudges, everything else is fixed by replying to the task's recovery comment), `pause`/`resume`/`takeover` (teams don't pause — taking over is a comment plus `task claim`, which mechanically holds agents off via the same lease workers use, §7.3), `start`/`activate`/`deactivate`/`run`/`serve`/`--once`/`deploy` (a board has no lifecycle and is never executed by hand — `init` makes it live, the runner it installs keeps it live, `rm` takes it down; deployment to exe is init's placement step, not a verb), `tick` as vocabulary (the machine verb is named `sync`), `dlq`/`dlq replay` (→ `logs --failed` + conversational recovery), `plan`/`apply`/`verify`/`doctor` (→ init's always-preview + `status`), `gc` (automatic), `task filter` and `--status` (aliases; `--filter` is the one query path), `--smoke` (verification is init's default), `--as` (→ `--name`), `--holder`/`--ttl` on claim (derived), `--max-steps` (derived), `--runtime`/`--agent` (there is exactly one runtime: acpx, installed by init — no choice exists), `--local`/gitcrawl reads (v1.1 with the sqlite store), `app create` (folded into `init --on github`).

## 9. Agent runtimes and the GitHub store

### 9.1 Agent runtime interface (port of the proven CardAgentRuntime)

```go
type Identity struct { Flow, TaskID, Role, Stage string; Attempt int; Host, Workdir string }
func (id Identity) SessionName() string  // pure derivation
func AssertIdentity(id Identity) error   // re-derive + compare — EVERY call passes through this

type Runtime interface {
    Ensure(ctx, Identity) (Session, error)  // idempotent reattach by derived name
    Send(ctx, Identity, Prompt) (Turn, error)    // in-process turn
    Enqueue(ctx, Identity, Prompt) error         // launch on a remote worker (launched work mode)
    Status(ctx, Identity) (Status, error)
    History(ctx, Identity, limit int) ([]HistoryEntry, error)
    Cancel(ctx, Identity) error
    Close(ctx, Identity) error
}
```

SessionPolicy → name: `continue_task` = `flow-<board>-<task>-<role>`; `continue_stage` adds `-<stage>`; `new_attempt` adds `-a<attempt>`. **TaskID is optional**: board-scoped turns (the supervising manager) run in `flow-<board>-<role>` — Turtleflow's repo-scoped manager session, carried. Because `Ensure` is idempotent reattach, **agent memory survives runner death for free** under `continue_*` (invariants 12/13 — Turtleflow's strongest property, carried verbatim).

**Branch identity is derived too**: `Identity.BranchName()` = `flow/task-<id>`, asserted like session names. The publisher commits exactly there, and the GitHub store correlates `check_run`/PR events back to their task by parsing it (Turtleflow's `codex/issue-N` join key, generalized) — this is what makes `ci-completed`/`pr-*` routable events rather than noise.

**The agent runtime is acpx — there is exactly one.** A direct Go port of `acpx.ts` (~300 lines): `sessions ensure/status/history/cancel/close`, pinned-version assert before any work. `init` installs it wherever agents run — on worker boxes via the bootstrap (as Turtleflow does) and locally the same way — so "which runtime" is not a choice anyone makes and no fallback half-runtime exists. The exe variant is the same runtime with its command runner wrapped in `ssh <host> -- sudo -u <user> …` (the literal `SshCardAgentRuntime` pattern); missing VM → typed `ErrHostAbsent` → a `needs_attention` finding, never a crash. **Not** the `prompt` button runtime: verified pull-model (`runner.go:77` just resolves a doc path) — no sessions, no status/cancel, no continuity.

Two internal work modes (never user vocabulary): in-process turns (local default; lease TTL = `TimeoutSeconds` + grace) and launched turns (exe default; `Enqueue`, journal `step.launched`, later ticks poll `Status` → collect → parse verdict → release; TTL expiry → `Cancel` + retry).

No agent configured = a durable `needs_attention: no-agent-configured` condition surfaced in `flow status` with the exact fix command; timers/reconcile/sweep still run; self-clears on the first tick after it's fixed.

### 9.2 Provisioning (GitHub store)

`flow init --on github` ports Turtleflow's proven sequence (always plan-preview-confirm before touching anything): preflight (native device-flow auth with `gh auth token` fallback; **organization-owned repo required** — org issue fields demand it); Project select/create + link; **plan/apply/verify with plan-hash + operation receipts, ported whole** — `Read → Plan → Apply(planHash)` with a fresh re-plan hash assertion, **refusal of destructive operations**, post-apply re-plan that must be all-noop ("apply did not converge"), receipts JSONL + secret-free assertion; operations: Pipeline org field (options = stage titles, ordered, PATCH resends option IDs), Project Status options + one board view, the agent-state mirror fields (Agent Status, Manager Verdict, Agent Iteration, Next Heartbeat — presentation only, never truth), labels, App webhook config. `flow status` reports drift later; re-running `init` repairs it. Setup PR + wait-for-merge is the resume point; everything is read-diff-write idempotent and resumable from GitHub state alone (markers). The 27-prompt template tree is **not** ported — the FlowDefinition already carries stage prompts, and skills come via registry `requires`.

### 9.3 Task mapping (GitHub store)

Issue = task; one board = one repository (numeric id; name asserted against it on every event). `props.status` = the **org-level Pipeline custom issue field** — the single source of truth, re-read live on every routing decision (`status` values are stage **IDs** everywhere in the engine; the adapter maps IDs ↔ the field's display titles, both ways); Project board `Status` stays a **mirror with relay-don't-act** (a board drag is relayed into Pipeline and does no work itself). Tags = labels (+ the reserved `flow:*` pair). WorkLog = the pinned work-state comment. Other props = labels `p/<key>/<value>` with the store enforcing single-valuedness (flagged: decided-unless-objection; alternatives were Project fields — mirrors, wrong home — and body front-matter — edit-collision-prone). Adapter-private: markers, field IDs, pinned comment, node_id resolution, linked branches, Status writeback. Revision = hash(updated_at + labels); GitHub has no CAS, so guarded updates are engine-checked and correctness leans on the enforced singleton lease.

### 9.4 Ingress + App identity

Buttons' existing webhook `server.go` is a correlation-ID one-shot waiter (drop-if-no-waiter) — the opposite of durable; only the **tunnel transport** (`tunnel.go`/`cfapi.go`) is reused, fronting a durable ingress in the runner: bounded body → raw-HMAC verify (constant-time) → O_EXCL claim + dual idempotency → authorize (collaborator gate; bot allowlist; echo suppression; `/flow …` comments parsed into operator events) → **ack-before-dispatch** (202 after claim, then deferred dispatch — a 90-minute turn can never blow a webhook timeout; **the acknowledgement is visible**: the store posts/updates a reply comment on the task with queue position + live transcript link and refreshes the pinned status comment — Turtleflow's `acknowledgeCardEvent` — and the verification gate's "ack ≤10s" (§11) measures that reply, not the HTTP 202) → scheduler → bounded retries → pinned-failure-exactly-once → restart replay. User-facing wording for all of this: "listening for GitHub events" — the word ingress stays in this document. `projects_v2_item` payloads lack `repository`; route by (installation, org, project number) with ambiguity as a hard error.

App identity: **BYO App per org**, created inline during `flow init --on github` when none is registered (manifest flow — Turtleflow's dormant `github-app-setup.ts` is the reference implementation) with the exact permission set baked in; **`assertAppCapabilities` ports verbatim — any extra permission is a hard failure**. Pem 0600; installation tokens minted + cached in memory (5-min skew); the user's token authorizes the human once, then is discarded. Registration store + installation-event reconciliation port as-is.

### 9.5 Exe worker + publisher

Core interfaces: `WorkerRuntime` (EnsureEnvironment/Send/Enqueue/Status/Cancel/Collect/Cleanup over derived-then-asserted `WorkIdentity`) and `Publisher` (the runtime commits; **workers never push**). Exe adapter keeps, verbatim in spirit: VM naming `bf-<repo>-<task>` with strict assert regex; lazy provisioning behind capacity checks; bootstrap with npm integrity gates + pinned runtime asserts; **credential-free checkout** (git bundle over scp, credential helpers stripped — the box can neither fetch nor push); prompt composed at dispatch and piped over SSH stdin, never stored on the box; `Collect` = diff between sentinels, never authoritative; **trusted publisher** (GitHub adapter behind `Publisher`): Git Data API commits with the App token, protected paths, secret patterns, declared-paths equality, artifact-hash equality, head-still-current ("rebase and review again"); **VM-comment lifecycle state** + 5-minute bounded sweeper (3 attempts → preserve + escalate; retains on open/`flow:keep`/escalated; refuses name mismatches). Run logs are **core** (runner-side): content-addressed `runs/<repo>/<task>/<runId>.html` + SSE, HMAC bearer tokens, `run-*` lifecycle events dispatched back through the ingress, bounds 20/14d/50MiB (rendered artifacts with their own bounds, separate from §7.9's raw turn records), failed runs still render. The local adapter implements the same `WorkerRuntime` against a sandbox dir per task — proving the interface cut.

Runner placement on exe (init's install step, per decision 5): uploaded bundle, systemd unit with `ProtectSystem=strict`, health gate on deployment SHA — the `gateway-deploy.ts` pattern generalized. **An exe.dev account is a preflight requirement** of `init --on github` — both the runner VM and the per-task VMs are created via `ssh exe.dev new`, exactly as Turtleflow does; init checks this before touching anything.

**The setup/verify seam (explicit, or the build trips on it):** the core exe adapter provides the machine, the checkout, and the collect/publish path — nothing project-specific. What commands install a repo's dependencies on the box, and what commands verify the work before the publisher accepts it (Turtleflow's `apps{setup[], verify[]}`), are **template configuration** — the swe template ships them as stage capabilities; a furniture board has none and needs none. The publisher's re-run-verify-before-publish behavior is core; the verify commands themselves come from the template. **Environment bindings ride the same seam**: how configuration reaches a credential-free box — named bindings with a security class (`public`/`generated-local`/`derived`/`network-capability`/`controller-only`/`forbidden`, Turtleflow's `EnvironmentBindingSchema`) and per-app scoping — is template/board config the exe adapter consumes at bootstrap; the class taxonomy (what may ever reach a worker) is core, the bindings are config.

## 10. FlowDefinition schema v3 — the team, gates, capabilities, stage prompts, board config

`SchemaVersion` → 3; readers accept 1 and 2 unchanged (v2 drawers simply have the new fields nil). All extensions share the versioning mechanics: the authoring CLI bumps `schema_version` only when a v3 field is first set; `flow_normalize` includes them in the content hash; `validate.go` rejects them under v<3.

### 10.1 Gates

Turtleflow's `requires_human_approval` has no home in the shipped format; without it a gated stage cannot stop a manager verdict from advancing:

```go
type FlowGate struct {
    RequiresHumanApproval bool     `json:"requires_human_approval,omitempty"`
    Approvers             []string `json:"approvers,omitempty"` // empty = any Authorizer-approved actor
}
// FlowStage gains: Gate *FlowGate `json:"gate,omitempty"`
```

Gate semantics: an advance verdict out of a gated stage does **not** write `status` — the engine sets `flow.pending_approval=<to_stage>` (surfaced as `needs_attention: approval-pending`), records the verdict bound to its journaled evidence, and surfaces the request. Only `buttons flow approve TASK` (Authorizer + Approvers checked, operator priority 100) commits; `flow reject` refuses — your comment says why, and the manager decides where the task goes. Heartbeats, retries, and replays mechanically cannot bypass — the commit path is exclusively the approval event.

### 10.2 Roles — the named team

Turtleflow's team is a registry of named roles (manager, lead, builder, reviewer, qa, release, platform — `RoleAgentSchema`, `packages/contracts/src/schemas.ts`), each with its own configuration. The shipped format collapsed this to anonymous manager/worker refs; v3 restores the team:

```go
// FlowDefinition gains: Roles map[string]*FlowRole
type FlowRole struct {
    SystemPrompt   string            `json:"system_prompt,omitempty"`
    Model          string            `json:"model,omitempty"`         // which LLM; empty = runtime default
    Capabilities   *FlowCapabilities `json:"capabilities,omitempty"`  // composes with board/stage (§10.4)
    Heartbeat      *FlowHeartbeat    `json:"heartbeat,omitempty"`     // {EverySeconds, StaleAfterSeconds}
    Cron           []FlowCronJob     `json:"cron,omitempty"`          // {Name, Cron} — role-owned schedules
    Watches        []string          `json:"watches,omitempty"`       // out-of-band kinds this role hears: run-* and health-changed (echo-suppressed otherwise)
    TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
    Escalation     *FlowEscalation   `json:"escalation,omitempty"`    // {MaxAttempts, ToStage}
}
```

- Stage `Manager`/`Worker` refs may now name a role — `role:reviewer` — alongside the existing `activation.*` and `agent:*` forms. Define "reviewer" once; use it in three stages.
- `StaleAfterSeconds` is the configurable staleness threshold behind the `stalled-worker` finding (Turtleflow default 45 min, min 5).
- Role schedules produce **board-scoped** events (`hb:<flow>:role:<role>:<bucket>`, `cron:<flow>:role:<role>:<scheduled>`) whose turns run in the role's board-scoped session (§9.1). `FlowManager.HeartbeatSeconds` is simply the manager role's heartbeat — one mechanism, not three; stage heartbeat triggers remain the work tick.
- **Escalation is promoted from "later" to v3**: when a role exhausts `MaxAttempts` on a task, the task moves to `Escalation.ToStage` (validated to exist — typically an authored "escalated" stage the cleanup sweeper retains). This is the one deliberate refinement of *failure never advances*: failure never advances a task **along the pipeline**; configured escalation may move it **to the escalation stage**, exactly as Turtleflow's `escalation.target_column` does.
- **No permissions, no sandbox settings — by decision.** Every role runs full-access; there is no read-only mode and no capability enforcement layer. Safety lives in the publish path and decision validation (§4).

### 10.3 Stage prompts — feedback, review, evidence

Turtleflow gives every column two jobs: the work itself, and **responding to feedback** (a human comment). The shipped `SystemPrompt` already is the work prompt; v3 adds only the second:

```go
// FlowStage gains:
OnFeedback string `json:"on_feedback,omitempty"` // prompt for answering a comment on a task sitting here (same form as SystemPrompt)
Review     *FlowReview      `json:"review,omitempty"`      // {Skill, MaxIterations (default 3)} — review loop bound; exhaustion follows the role's escalation (§10.2)
Evidence   *FlowEvidence    `json:"evidence,omitempty"`    // {Skill, Required []string} — proof the work must produce
```

`OnFeedback` composes over the stage's `SystemPrompt`; a stage without it answers comments with the work prompt. `Review`/`Evidence` extend `Completion` the way Turtleflow's `column_jobs[].review/evidence` do: `Evidence.Required` names artifact kinds that must be **present in the turn's journaled evidence bundle** before an advance is accepted — checked engine-side against the journal, never echoed by the agent (§7.6).

### 10.4 Capabilities — skills, tools, prompts per node

The shipped format carries a system prompt per stage and per manager — and nothing else. Turtleflow's roles carry `skills[]` and `tools[]` (`RoleAgentSchema`, `packages/contracts/src/schemas.ts`), and they are load-bearing: what an agent may know and may touch is per-node configuration, not global. v3 adds:

```go
type FlowCapabilities struct {
    SystemPrompt string   `json:"system_prompt,omitempty"` // composes with, not replaces, the inherited one
    Skills       []string `json:"skills,omitempty"`        // skill packages (SKILL.md), resolved via the registry
    Tools        []string `json:"tools,omitempty"`         // button names this node's agents may press
}
// FlowDefinition gains: Capabilities *FlowCapabilities  (board defaults)
// FlowStage gains:      Capabilities *FlowCapabilities  (extends the board's)
```

Inheritance is downward and additive: board → stage → role → task. Stage capabilities extend the board's; roles bring their own (§10.2); a task adds via the user-settable props `skills` / `tools` / `worker`. The engine composes the effective set at dispatch: system prompts concatenate board → stage → role (+ `on_feedback` for comment events); skills resolve like `requires`; the tools list is composition, not enforcement — full-access by decision (§4).

**How tools reach a worker:** the buttons binary (single static Go) plus the composed tool buttons are installed on the worker's machine by the box bootstrap (locally they are already present); the agent presses them locally, inheriting the JSON contract, timeout, and `pressed/` history. **Board agents never use the task CLI:** a worker's machine is credential-free and cannot reach the store — workers receive composed prompts and answer in conversation and verdicts, and the engine mediates every state change (invariant 20, made explicit). The task verbs exist for humans and for agents *outside* the board (a personal assistant filing tasks).

### 10.5 Board config — intake, composition, policies

```go
// FlowDefinition also gains:
TicketPrompt string        `json:"ticket_prompt,omitempty"` // how a raw new task is structured + enriched at intake (type/priority/`scope` inference — Turtleflow's ticket_prompt)
Composition  []string      `json:"composition,omitempty"`   // ordered prompt recipe; default in code:
                                                            // [current-task, current-event, conversation, repository-instructions, response-format]
Policies     *FlowPolicies `json:"policies,omitempty"`      // {CleanupGraceHours, AbandonedRetentionHours} — sweeper knobs, no longer hardcoded
```

Later v3 candidates: `FlowLimits.MaxRunningTurns` (v1 conflates task-WIP with turn concurrency via `MaxActiveTasks`), `FlowStageTrigger.Priority`.

## 11. Acceptance — one deliverable, no phases

Buttons Flow ships whole. There is no partial milestone that counts as done while the rest waits: the deliverable is the complete v1 — contract, journal, leases, scheduler, engine, runner, both stores, both worker adapters (exe + local sandbox), the registry template, provisioning, the App flow, the exe worker, and the verified proof. (The recon that would have been a "phase 0" is §2 + Appendix A, already verified.) Everything below must pass, **demonstrated — run the commands, paste the output** — in the existing integration harness (real binary, isolated `BUTTONS_HOME`; `test/integration/flow_drawer_test.go` is the template; tests may invoke the machine verb directly). The conformance suite runs identically against both stores; that suite is what makes the swappable-store thesis real.

**Contract & journal**
- Two concurrent `task claim` → exactly one wins; loser exits `CONFLICT`, message "already claimed by X until <time>".
- Holder A expires; B acquires (token+1); A's guarded update → CONFLICT; A's renew fails.
- Same logical event under two delivery IDs → second is duplicate via the event-id index — including a webhook firing and a reconcile observation of the same change.
- Handler fails 3× → pinned as failed once; `needs_attention` set; `status` untouched; a recovery comment lands in the task's conversation; `flow logs --failed` lists it; a subsequent `task comment` re-dispatches exactly once; a crash during failure reporting re-reports exactly once on restart.
- WorkLog: same eventID twice merges to one entry; 25 appends retain newest 20; corrupt file reads empty.
- Unknown props/tags round-trip untouched; update merges; `rm` twice is `removed:true` then `removed:false`, never an error.

**Engine & board**
- `flow init` on the local store installs the runner as a user service; `task add` → agents drive the task to a terminal stage with no further human commands.
- **Flagship: `kill -9` the runner mid-turn → service restart: run `interrupted`, delivery re-dispatched exactly once, `status` unchanged, session reattaches under `continue_*`.**
- 10 ready tasks, `MaxActiveTasks=2` → never >2 concurrent turns; per-task serialization holds; operator cancel preempts.
- 5 heartbeat firings in one bucket → one journal record; heartbeat-origin advance verdict → rejected, status unchanged.
- A malformed verdict gets a bounded contract-correction re-ask (2) before the attempt is charged; only after those fail does the turn count as a failed attempt, raw output kept in the conversation.
- Drawer edited under a live board → `FLOW_CHANGED` refusal with the `--upgrade` hint; `init --upgrade` clears.
- A human `task claim` keeps agent turns off the task (per-task serialization over the same lease); comments during the claim are journaled, not lost; expiry hands the task back and the queued work runs.
- Gated stage: advance sets pending approval; `approve` commits; unauthorized actor ignored pre-claim; `/flow approve` as a comment behaves identically on both stores.
- Roles: a stage referencing `role:reviewer` gets that role's prompt/model/capabilities; a task's `worker=` prop reroutes its turns; unknown name → `needs_attention: unknown-worker`, no dispatch.
- on_feedback: a comment on a task runs the stage's feedback prompt, not its work prompt; a stage without `on_feedback` falls back to the work prompt.
- Escalation: a role exhausting `MaxAttempts` moves the task to `Escalation.ToStage` exactly once, with the reason in the conversation; a `delegate` verdict dispatches the named role with its instruction and advances nothing.
- Capabilities: a stage's skills/tools compose over the board's; a task's `skills` prop adds one more; the composed set is visible in the turn's journal record.

**Template & registry**
- In a clean workspace, `buttons add <pkg>` then `buttons flow init <name>` succeeds end to end — including the default verification task.

**GitHub & exe**
- `flow init --on github` provisions idempotently (re-running init converges to all-noop); destructive ops refused; receipts written; the plan is printed and confirmed before any real resource is created — declining changes nothing.
- Board drag relays into Pipeline and triggers exactly one dispatch (relay-don't-act).
- The same flow drawer runs **unmodified** against GitHub — the store thesis, demonstrated.
- A `check_run` completion on a task's derived branch reaches the stage subscribed to `ci-completed`.
- The verification gate: `init --on github` drives a throwaway marked issue through the first real transition + one feedback round with **ack ≤10s** (the visible reply comment) **and warm worker start ≤10s**, asserts same-session feedback, deletes the issue — and reports success only after. Provisioned == working.

**Definition of done** — in a clean workspace:

```
buttons add @buttonsflow/swe
buttons flow init swe                        # provisions, installs the runner, proves it — the board is live
buttons flow task add "login page 500s on Safari"
buttons flow task list --filter status=done  # agents worked it; nobody ran anything
```

…and `init --on github` makes the same board live on GitHub + exe, running the identical drawer unmodified, `kill -9` of the runner at any point included.

## 12. Risks and open questions

1. **Turn I/O contract** — the exact JSON a manager/worker button receives and returns. §7.6's verdict schema is proposed; needs one review round with Bob during the build.
2. **Heartbeat semantics** — v3 unifies role schedules (board-scoped supervision, incl. `FlowManager.HeartbeatSeconds` as the manager role's heartbeat) vs stage heartbeat triggers (work ticks); confirm the split holds during the build.
3. **Attempt accounting under crash** — journaling `step.started` pre-spawn charges an attempt even if the agent never ran. Conservative default; acpx `History` lookup by eventID could make it exact. Decide during the build.
4. **GitHub revision derivation** — no true CAS; revision = hash(updated_at + labels) with engine-side read-verify-write, safe under the singleton lease. Verify the derivation against real webhook timing during the build.
5. **Cross-machine leases** — file leases fence one host; store-anchored leases (GitHub verify-after-write) are v2. The interface is already shaped for it.
6. **`buttons task` at top level** — v1 keeps one spelling (`buttons flow task …`); promote a top-level alias in v1.1 once the noun has earned it.
7. **Retention defaults** (journal 14d/2000, runs 30d/500) — taste call, revisit with usage.
8. **Org-owned repos only** (org issue fields require it) — inherited in v1; personal-account support would need a different status carrier.
9. **Comment-command grammar** — `/flow approve|reject|claim|cancel` parsing rules (quoting, arguments, unknown commands) need one tight spec during the build; unknown `/flow` commands must be ignored-with-reply, never guessed.
10. **Service-manager surface area** (launchd/systemd user units for the local runner) — small but platform-fiddly; the fallback if it fights back is a supervised background process + crontab entry, same tick underneath.
11. **Is this a product?** Like Turtleflow before it, most of v1 is convention + runtime rather than new surface area. That is the design working, not a gap — flagged so it isn't discovered late.

---

## Appendix A — The 35 Turtleflow invariants (normative)

Each invariant names its Buttons Flow mechanism. "Port" = carried unchanged; "upgrade" = strengthened; sources are `autonoco/turtleflow` at the paths noted.

| # | Invariant (source) | Buttons Flow mechanism |
|---|---|---|
| 1 | Webhook never processed twice across crash — O_EXCL create is the claim (`github-webhook.ts:367-378`) | Journal record O_EXCL create (§7.2) — port |
| 2 | One logical event under two transport IDs accepted once (`:363-366`) | Event-id index, O_EXCL, O(1) (§7.2) — upgrade (was O(n) scan) |
| 3 | Ack before any long work (deferred dispatch, `:213-223`) | 202-after-claim ingress; `dispatched` checkpoint before spawn (§7.5, §9.4) — port |
| 4 | Crash resumes every in-flight delivery on restart (`:446-490`) | `ReclaimRecoverable` on runner startup (§7.2) — port |
| 5 | Dead-letter exactly once even if reporting fails (`:272-288`) | `DeadLetterReportPending` durable marker (§7.2) — port |
| 6 | A failure never advances or loses a task (`gateway.ts:1346-1418`) | `status` written only on validated advance; failures set `needs_attention` (§7.5); configured escalation to its target stage is the one deliberate exception (§10.2) — port |
| 7 | One task runs at most one turn at a time (`scheduler.ts:63-70`) | Scheduler per-key serialization (§7.4) — port |
| 8 | Board-wide concurrency hard-capped (`scheduler.ts:64`; `gateway.ts:3251`) | `MaxActiveTasks` global cap + stage `Concurrency` (§7.4) — port |
| 9 | Operator actions jump the queue (`gateway.ts:2926`) | Priority tiers, operator=100 (§7.4) — port |
| 10 | A human turn queues behind a held task with no queue infra (`ui.ts:1238`) | Store event → same task key in the scheduler — port |
| 11 | Two processes never take the same task (topology) | Singleton board lease + runner.lock (§7.3) — **upgrade: enforced, not assumed** |
| 12 | Agent memory survives orchestrator death (`acpx.ts:59-79`) | Idempotent `Ensure` reattach by derived name (§9.1) — port |
| 13 | An agent can never target another task's session (`card-agent-runtime.ts:130-156`) | `AssertIdentity` on every runtime call (§9.1) — port |
| 14 | Full run history reconstructable from the store alone (`ui.ts:570-589`) | Contract-level WorkLog (§6.2) — port |
| 15 | Concurrent writers cannot fork the status record (`github-app.ts:760-779`) | WorkLog merge-by-key onto one record (§6.2) — port |
| 16 | Corrupt work-log degrades to empty, never crashes (`:2021-2023`) | `ReadRuns` parse-fail → empty (§6.2) — port |
| 17 | Reprocessed event updates, never duplicates, its run row (`:1455-1492`) | `Key = sha256(eventID)` merge (§6.2) — port |
| 18 | The bot cannot trigger itself (`github-webhook.ts:167-179`) | Bot allowlist + marker filtering in ingress (§9.4) — port |
| 19 | A run's lifecycle events can't wake the session that produced them (`gateway.ts:5245-5270`) | `Origin.Session` admission filter (§7.7) — port |
| 20 | An agent cannot move a task (`gateway.ts:1208-1221`) | Only the engine writes `status`, only on validated advance (§7.6) — port |
| 21 | A manager cannot exceed granted authority (`board-manager-runtime.ts:47-84`) | `decide` re-validation from the FlowDefinition (§7.6) — port |
| 22 | A reviewer cannot pass an artifact it did not see (`manager.ts:19-31`) | Verdicts bound engine-side to the journaled evidence of the turn that produced them — agents never copy hashes (§7.6) — port, mechanism improved |
| 23 | A heartbeat can never advance a task (`gateway.ts:2592-2599`) | Heartbeat-origin verdicts lose `advance` (§7.6) — port |
| 24 | A gated stage cannot be left without explicit human approval (`gateway.ts:1176-1198`) | Schema-v3 Gate + approve-only commit path (§10) — port |
| 25 | Overlapping heartbeat ticks collapse into one (`gateway.ts:1477-1505`) | Bucketed IDs + journal dedup + `Depth()` guard (§7.1) — port |
| 26 | One heartbeat window = one logical event (`gateway.ts:2782-2809`) | `hb:<flow>:<stage>:<bucket>` event IDs (§6.3) — port |
| 27 | Dead/stalled workers surface without anyone watching (`gateway.ts:2512-2529`) | Sweep: lease expiry → abandoned → `needs_attention` (§7.1) — port |
| 28 | Cleanup never deletes work someone still wants (`gateway.ts:3275-3403`) | Sweeper retains on open/`flow:keep`/escalated; bounded attempts (§9.5) — port |
| 29 | Cleanup state survives restarts because it lives on the resource (`:3296-3300`) | VM-comment lifecycle state (§9.5) — port |
| 30 | No durable store grows without bound | Retention bounds on journal/runs/WorkLog, pruned automatically (§7.9) — port |
| 31 | A partial write can never be observed (`gateway.ts:5465-5477`) | tmp+rename 0600 everywhere — port |
| 32 | Run log URLs unguessable + content-addressed (`gateway.ts:5449, 7014`) | HMAC bearer tokens, `runId = sha256(eventID)` (§9.5) — port |
| 33 | A failed run still produces a readable transcript (`gateway.ts:6968-6990`) | Append-only `.log.jsonl` + checkpointed states (§7.5) — port |
| 34 | Signature verification before parsing, before authorization (`github-webhook.ts:159-160`) | Raw-body HMAC first in the ingress (§9.4) — port |
| 35 | Runtime version drift caught before any work (`acpx.ts:252-267`) | Pinned acpx assert + `FLOW_CHANGED` hash gate (§7.1, §9.1) — port |

Closed gaps (things Turtleflow lacked, added here): reconciliation poll for never-delivered webhooks (tick phase 2); TTL'd fencing leases (§7.3); O(1) logical dedup (§7.2).
