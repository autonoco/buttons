# Buttons Flow — PRD

**Status:** Draft for build (v2 — architecture replaced per Eric's review on PR #264, 2026-08-01)
**Owner:** Bob
**Date:** 2026-07-31 (v1) / 2026-08-01 (v2)
**Companion:** `buttons-flow-agent-prompt.md` (the prompt to hand a coding agent with this PRD)

---

## 0. What changed from v1

v1 specified a new durable runtime — `internal/flow/` + `cmd/flow.go`: a journal, a TTL-fenced lease store, a scheduler, a runner daemon (user service locally, an exe.dev VM + GitHub App on GitHub), run checkpoints, and Go `Store`/`Runtime`/`Backend` interfaces. Eric's review on PR #264 (two comments, 2026-08-01) argued that none of it is necessary: a flow drawer can be a **stateless drawer built from ordinary buttons** that presses itself. Verified against the code, he's right — the primitives already exist:

- `internal/queue/queue.go` is a keyed flock semaphore. `key=<task-id>, concurrency=1` is already a single-host per-task mutex — no new lease type needed there.
- Idempotency (`--idempotency-key`) is documented (`CLAUDE.md`) as lookup → execute → store — a result cache, **not** a compare-and-swap. Eric flagged this explicitly; it must not be used as the coordination primitive.
- The drawer executor already implements `button`, sub-`drawer`, `for_each`, `switch` (CEL), `aggregate`, and `wait` steps (`internal/drawer/executor.go`) — real composition, not placeholders.
- A `runtime: "prompt"` button is already the pull-model agent-task primitive (`internal/runner/runner.go:77`) — sessions, an injected system prompt, structured output. This is the "existing agent/runtime button" Eric points at; nothing new is needed to make an agent do a turn.
- Drawers already support a webhook trigger, end to end (`cmd/drawer.go`, `buttons webhook listen`) — external events can press a drawer today.

**v2 (this document) is the replacement, not an amendment.** `internal/flow/`, the lease/journal/runner Go types, the GitHub App identity flow, and the exe.dev runner VM are all cut. A `FlowDefinition` now compiles to a pipeline of steps in memory, on every press, and is handed straight to the existing executor (§1) — no second drawer, no persisted artifact. The `drawer_kind:"flow"` entity itself is unchanged and, per an existing invariant, still never carries steps on disk.

## 1. Summary

Buttons Flow makes `drawer_kind: "flow"` pressable. Today pressing one returns:

> `FLOW_RUNTIME_REQUIRED: flow drawer %q is activated and run by a Buttons Flow runtime; it cannot be pressed as an action drawer` (`cmd/drawer.go:475-480`)

This PRD replaces that refusal with a compiler — an **in-memory one, run fresh on every press, never persisted**. The `FlowDefinition` (`internal/drawer/entity.go:93-153`, already shipped, validated, hashed, registry-distributed) stays exactly what it is on disk: a `drawer_kind:"flow"` entity that never carries `steps` — `Drawer.MarshalJSON` (`entity.go:72-88`) strips `steps` from any flow-kind drawer by design, and this PRD does not touch that invariant. It doesn't have to: that invariant only fires at *serialization*, and this design never serializes a compiled pipeline. `cmd/drawer.go`'s `drawerPress` and `cmd/serve.go`'s webhook dispatch (§6.4) both detect `DrawerKind == "flow"`, call one shared compile function that builds the pipeline (§6) as an **in-memory `Drawer` struct carrying real steps**, and hand it straight to the unmodified `internal/drawer/executor.go`. Nothing is ever written back to `NAME`'s own `drawer.json`; no second drawer entity is created, named, or cleaned up. `buttons flow init NAME` doesn't generate anything — it registers the board's webhook trigger and polling schedule (§6.4) directly on the flow drawer itself, then presses once to prove the compile-and-execute path works end to end, the same path every later press (manual, webhook, or scheduled) also takes. No daemon, no journal, no lease store, no persistent process — and now, no second drawer either.

**A board is one drawer that compiles itself fresh, on every press.** Every press is a complete, independent turn:

```
provider-list → for each actionable item: claim → perform → validate → apply → (ensure re-press is scheduled)
```

Nothing survives between presses except what the *provider* holds — a GitHub issue's fields and labels, or a local task file. Buttons owns no journal, no cursor, no lease store, no scheduler. Local and GitHub boards compile the same `FlowDefinition` to the same pipeline shape; only the provider/claim buttons the compiler selects differ.

**Vocabulary rule (still normative):** the CLI speaks plain words — **init, task, status, logs, approve**, with `reject`, `cancel`, `rm` — every one of them is now sugar over an existing primitive (press, history, summary), not a new subsystem. There is no start, stop, run, retry, or pause — not because a runtime forbids them, but because there is no runtime to start, stop, run, retry, or pause. Killing a press mid-turn loses at most that one turn; the next press re-reads the provider from scratch.

## 2. Background

### 2.1 What Buttons already has (verified against code, unchanged from v1)

- **The flow definition format** (`internal/drawer/entity.go:93-153`): `FlowDefinition{InitialStage, Manager{Agent, SystemPrompt, HeartbeatSeconds}, Limits{MaxActiveTasks, MaxRuntimeSeconds, MaxAttemptsPerStage}, Stages[]{ID, Title, SystemPrompt, Manager, Worker, SessionPolicy, Triggers(heartbeat|cron|event|webhook), Transitions, Completion, Retry, TimeoutSeconds, Concurrency}}`. Authoring (`buttons drawer create X --kind flow`, `stage add`, `set flow.*`), validation (`validateFlowDefinition`, `internal/drawer/validate.go:334-460`), normalization + content hashing (`flow_normalize.go`), registry publish/install with byte-verified `flow-definition.json`. **This does not change in v2** — same format, different runtime underneath it.
- **Action drawers, real composition today**: `internal/drawer/executor.go` implements `runStep` (button), `runDrawerStep` (sub-drawer), `runForEachStep` (with `Parallelism` and `OnItemFailure`), `runSwitchStep` (CEL predicates), `runAggregateStep`, `runWaitStep` — retries with backoff, `on_error`, full CEL `${step.output.field}` data flow, a parallel wave executor. This is the entire mechanism v2 compiles into — and it's also where "typed inputs and outputs" and "packaging" (Eric's terms) live: `InputDef`/`OutputSchema` type drawer boundaries, and the registry is the packaging/distribution layer (§2.1 below).
- **A keyed concurrency primitive**: `internal/queue/queue.go` — file-lock slots scoped by `name` + optional `key`. `{name: "flow-claim", concurrency: 1, key: "<task-id>"}` is a single-host mutex per task, no daemon, survives across CLI invocations by design (its own doc comment: "No daemon needed").
- **A pull-model agent runtime**: `runtime: "prompt"` buttons (`internal/runner/runner.go:77`, `internal/agentdoc`) — an agent task with an injected system prompt, run through the same press/queue/history/timeout contract as any button. This is the "perform the turn" primitive; v2 adds no new agent runtime. Buttons can equally wrap local code or remote APIs (shell/HTTP runtimes) — the "ability to wrap local code or remote APIs" Eric names is how provider/claim buttons reach GitHub or a local file with no new mechanism.
- **Existing triggers**: drawers already support a live webhook trigger (`cmd/drawer.go`, `buttons drawer NAME trigger webhook`, `buttons webhook listen` — 202-accepted, async press, constant-time auth). Separately, buttons (not drawers) support cron and file-watch triggers under `buttons serve` (`internal/trigger/`, `robfig/cron`). Neither needs to change for v2 — see §6.4 for how a flow board uses each.
- **A registry** whose packages can contain drawers, with transitive `requires`.
- **Batteries** (`internal/battery`) — shared env/credential injection: every resolved battery key lands in every press's env (`BUTTONS_BAT_<KEY>`). This is how a GitHub provider button gets a token without declaring `$ENV{...}` itself: one `github` battery, resolved once, read by whichever provider/claim button needs it.
- **JSON CLI contract** (`{"ok":…}` envelope), `pressed/` history — this *is* the run log; no separate WorkLog type is needed.

### 2.2 What's actually missing (all of it button-shaped, none of it a new subsystem)

1. The compiler: a shared function that turns a `FlowDefinition` into the in-memory composed pipeline described in §6, called from both `drawerPress` and the webhook dispatch path, replacing today's unconditional `FLOW_RUNTIME_REQUIRED` — nothing persisted, nothing generated, nothing serialized (the flow-kind entity structurally cannot carry `steps` on disk, §1, so this design never asks it to).
2. A documented, reusable **claim button pattern** (§6.3) — not a Go type, a convention any provider's claim button implements.
3. A **self-press guarantee**: `flow init` makes sure a webhook trigger (event-driven) and/or a recurring press (polling) exist for the board. The recurring press needs no new engine — an OS cron entry, a launchd/systemd unit, or (on GitHub) an Actions `schedule:` workflow can each invoke `buttons drawer NAME press` directly. `flow init` writes whichever fits the `--on` target; it does not run or supervise anything itself.

That's the complete gap. Compare to v1's §2.2 ("No durability... no resumability, no leases..., no daemon..., no durable timers..., no flow runtime") — every item on that list was a symptom of choosing to build a persistent-process architecture. A stateless, provider-backed design doesn't have durability gaps because it has nothing in memory to lose.

### 2.3 Turtleflow — prior art for vocabulary, not mechanism

Turtleflow (`autonoco/turtleflow`) runs agentic boards on GitHub Projects in production. v1 ported its mechanism (journal, TTL leases, a runner, checkpoints — the 35 invariants in its old Appendix A). **v2 does not port that mechanism.** What's worth keeping is the vocabulary Turtleflow validated in production: stages as a state machine, gates requiring human approval, named roles with escalation, a fixed manager verdict shape (`advance | retry | hold | delegate | escalate`). Those map cleanly onto a composed drawer's steps (§6, §7) without needing the durable engine Turtleflow built them on top of — Turtleflow didn't have buttons' composition, typed I/O, registry, or keyed queue to build on; we do.

### 2.4 Design decisions

The Jul 31 decisions (Bob + Eric) are superseded by Eric's Aug 1 PR review; **decisions below replace them**:

1. `buttons flow` **is** the missing runtime — still true — but the runtime is compilation to an existing primitive, not a new engine. No new definition format (unchanged from v1).
2. **No new Go packages.** No `internal/flow/`, no `Store`/`Backend`/`Runtime`/`lease.Store` interfaces. The "store" is whichever buttons a board's stages use to read/write provider state.
3. **v1 ships both providers** (local file, GitHub) — same goal as v1's decision 3, different mechanism: swappable buttons, not a Go interface with two implementations.
4. **No GitHub App.** GitHub-store buttons authenticate the same way any GitHub-wrapping button would (`gh` CLI token or a PAT via `$ENV{VAR}`, likely delivered as a battery, §2.1) — no manifest flow, no installation tokens, no App identity to provision.
5. **No installed runner, no exe.dev VM requirement.** Liveness is: a webhook trigger for event-driven presses (already shipped) plus a cron-style recurring press for polling, written by `init` to whatever the target platform's native scheduler is (crontab/launchd/systemd locally, Actions `schedule:` on GitHub). `init` never installs or supervises a long-running process for the *board* itself. (Isolated per-task execution environments — e.g., running an agent's turn on a sandboxed box — remain a legitimate, separate concern; if a board ever needs it, it's an implementation detail of the perform-turn button, not a platform requirement. Out of scope for v1.)
6. **Idempotency-key is not the coordination primitive** — explicit, because it was tempting to reach for and Eric's review specifically ruled it out (§2.1: it's a cache, lookup-then-store, not CAS). Coordination is the keyed queue (single host) or the claim protocol (§6.3, any number of hosts).

## 3. Goals

1. A published flow drawer is pressable the moment it's installed — `buttons drawer NAME press` compiles it in memory and runs one pass, no setup required, same as any action drawer. What `buttons flow init <drawer>` adds is *liveness*: after `init`, something presses the board on its own — a webhook, a cron entry, or both — and tasks move without further human commands. **A freshly `buttons add`-installed flow drawer works by hand immediately; it just doesn't press itself** until `init` registers a trigger for it — this is expected, not a bug, and matches how any other drawer's triggers need explicit registration after install.
2. An agent or human manages tasks with the same verbs regardless of provider (`add, list, read, update, rm, claim, comment`) — the verbs are the composed drawer's buttons; the provider is an implementation detail.
3. Kill a press at any moment and lose at most that turn: the next press re-reads the provider from scratch, and a claim older than its staleness window is treated as abandoned and re-claimable (§6.3). There is no journal to corrupt because there is no journal.
4. The same `FlowDefinition` compiles to the same compiled pipeline shape on the local provider and on GitHub — only the provider/claim buttons the compiler selects differ.
5. Provisioned == working: `init` proves the loop with one throwaway task before reporting success.
6. The surface is plain words (init, task, status, logs, approve, reject, cancel, rm) and every one of them is now visibly sugar over `buttons drawer press` / `buttons history` / `buttons drawer summary` — an agent reading the implementation should immediately see there's no hidden machinery.

## 4. Non-goals (v1)

- **No new Go packages or interfaces.** This is definitional in v2, not a scope cut — there's no journal, lease store, scheduler, or runner *type* to build, because the design has none of those *concepts*.
- **No permission model, no sandbox settings.** Agents run full-access, same as v1 — unrelated to the runtime question.
- **No event bus, no journal.** Provider state plus `pressed/` history is the entire record. A killed press simply didn't happen; the next press's `actionable` step finds the same work still there.
- **No GitHub App, no exe.dev VM requirement.** See §2.4 decision 5.
- **No UI beyond JSON.** `flow status`/`flow logs` JSON, plus whatever board UI the provider itself offers (a GitHub Project, if the board is wired to one).
- **No multi-provider per board.** One provider per board, same as v1.
- **Cut, and now unnecessary rather than deferred**: TTL-fenced leases, run checkpoints, dead-letter reporting, reconciliation polling, event-ID dedup index, singleton board lease, App manifest flow, exe worker/publisher interfaces. None of these solve a problem a stateless, provider-backed design has.
- **Deferred to v1.1**: a `ButtonStore`-style convention doc for third-party providers beyond local/GitHub; promoting `buttons task` verbs to a top-level alias.

## 5. Core concepts

| Concept | Definition | Implemented as |
|---|---|---|
| **Task** | The only noun. Work with props and tags. | Provider-native (a GitHub issue; a local task file) |
| **Comment** | The human↔agent channel on a task. | GitHub: issue comments. Local: a conversation file read by a `task-read` button. |
| **Property** | Single-valued key/value; the board branches/triggers on it. `status` mandatory. | GitHub: an issue field or `p/<key>/<value>` label. Local: a JSON field. |
| **Tag** | Multi-valued open label. | GitHub: labels. Local: a JSON array. |
| **Provider** | Where tasks live (local or GitHub) — whichever buttons a board's steps reference for read/write. | Ordinary buttons; no Go interface. |
| **Claim** | The best-effort "I've got this" marker on one task — race-checked, not atomic (§6.3 adopts a read-write-wait-reread protocol, not a CAS). | §6.3 — a documented button pattern, not a lease type. |
| **Board** | A flow drawer that presses itself. Live from `init` onward. | The compiled drawer + its triggers (webhook and/or a scheduled press). |
| **Flow drawer** | The definition of a board. Already shipped, unchanged. | `drawer_kind:"flow"` |
| **Template** | A packaged board definition + its requirements. | Registry package (exists today). |

Reserved namespaces (unchanged from v1, still useful vocabulary): props `flow.*` (`flow.attempts.<stage>`, `flow.claimed_by`, `flow.claimed_at`), the one user-visible signal `needs_attention` with a fixed reason vocabulary; user-settable `scope`, `worker`, `skills`, `tools` props. `deps` reserved for v2.

## 6. The composition — how a flow board actually runs

This is the mechanism. Every press of a flow drawer compiles its `FlowDefinition`, in memory, into this step pipeline (§1) — each step a `button`, `for_each`, `switch`, or `aggregate` step, all real today in `internal/drawer/executor.go` — and hands the compiled `Drawer` straight to it. `provider-list` runs once per press; everything from `actionable` through `apply` runs **once per claimed item, inside the `for_each` loop**; `ensure-trigger` runs once per press, after the loop:

```
provider-list   (button)   — read current state for this board's stages; runs once per press
   ↓
for_each item in provider-list.output.items      (bounded by Parallelism, §6.2's queue,
                                                    the stage's Concurrency, MaxActiveTasks):
  actionable    (switch)   — CEL: is this item in a stage whose triggers/transitions
                              make it workable now, and not currently claimed-and-fresh?
     ↓ (skip to next item if not)
  claim         (button)   — §6.3: atomically take this item, or skip it if the race is lost
     ↓
  perform       (button, runtime:"prompt") — the stage's Manager/Worker turn for this item;
                              composed system prompt = board → stage → role (§7)
     ↓
  validate      (button)   — parse + re-validate the verdict against FlowStage.Transitions
                              (§7.6's advance|retry|hold|delegate|escalate vocabulary, kept)
     ↓
  apply         (button)   — write this item's outcome back to the provider: status/labels/comment
   ↓ (after the loop, once per press)
ensure-trigger  (button, idempotent) — confirm the board's webhook + scheduled press are
                              still registered; a no-op after the first run
```

One press can therefore advance more than one task per pass — each claimed item gets its own independent `perform`/`validate`/`apply` turn, `on_item_failure: continue` so one item's failure doesn't block the rest of the batch.

### 6.1 Provider buttons

Local: a small pair of buttons over a JSON file under `~/.buttons/flows/<name>/tasks/` — `list`, `get`, `update` (merge-patch, matching v1's `Patch` shape conceptually but as plain button args, not a Go `Store` interface). GitHub: buttons wrapping `gh issue list/view/edit` (or the GitHub API directly) — `props.status` maps to an issue field or label, same mapping v1 already specified in its §9.3 and worth keeping verbatim as a mapping *convention*, just implemented as button logic instead of a `Backend`.

### 6.2 Single-host coordination

`claim` (and any provider write) declares a queue: `{name: "<board>-claim", concurrency: 1, key: "${task.id}"}`. Two presses racing on the same host serialize on the flock slot — no new mechanism, this is `internal/queue` exactly as it exists today.

### 6.3 Multi-host coordination — the claim protocol

For providers with multiple possible claimants (any GitHub board pressed from more than one place), the keyed queue doesn't help — it only serializes one host. Eric's protocol (PR #264, comment 2) is the mechanism, encoded directly as the `claim` button's logic:

1. Read the task's current details and assignee.
2. If it matches the claim criteria **and has no assignee** — write the claiming identity as assignee, labeled e.g. `Agent Claimed` (Eric's exact protocol).
3. Wait a short window (~1s) and re-check the task. If a concurrent claim overwrote it, move on to the next task (Eric's exact protocol).

This can be one `claim` button, or a set of buttons for a more granular contract (Eric's comment 2 offers both; v1 defaults to one button and splits only if a real need shows up).

**One addition beyond what Eric specified, flagged explicitly:** Eric's protocol only covers the concurrent-overwrite case — it has no answer for a claim that succeeded but then orphaned (the claiming process crashed mid-`perform`, and the task is now permanently assigned to nobody). Without *some* expiry, that task is stuck forever with no journal to notice it. So `actionable` additionally treats a claim older than a **staleness window** (default: the stage's `TimeoutSeconds`) as unclaimed again — a TTL-like check, in tension with comment 1's "no lease store," but it's a single CEL comparison in the `actionable` switch step, not a lease type, token, or fencing mechanism. Revisit if this proves unnecessary or wrong.

On the coordination primitive itself: Eric's comment 1 and comment 2 point at two different claim substrates, not two options for the same one. Comment 1 names "a ref/CAS operation on GitHub" — that's specifically **git refs** (`POST`/`PATCH .../git/refs/...`), which really is atomic on GitHub (ref creation fails if the ref exists; a ref update with an expected SHA fails if it's been moved). Comment 2's concrete protocol claims via **issue assignee + label** instead, and the Issues REST API has no conditional/If-Match write — there is no CAS available on that substrate, only the optimistic read-write-wait-reread pattern. v1 adopts comment 2's protocol, on issue assignee/label, as written — it needs no extra API surface and matches what a human reviewing the board sees (an assigned, labeled issue). If the race proves too wide in practice (§9), the fallback is switching the claim marker itself to a **git ref** (`refs/flow-claims/<board>/<task-id>`, one atomic create per claim attempt) — the actual mechanism Eric named — not a conditional write on the issue and not a lease store.

Idempotency-key remains available for a different purpose (deduping an identical retried press) but is explicitly not this mechanism (§2.1, §2.4).

### 6.4 Keeping the board pressed

`flow init`'s throwaway proof-press (§8) is what first exercises the pipeline's own `ensure-trigger` step (§6) — init doesn't have separate, bespoke registration logic; it's the same idempotent button every subsequent press also runs. This is the literal shape of Eric's "even setup and convergence are button functionality": setup isn't a phase `init` does *to* the board, it's the board converging itself, once by hand and every time after by press. Both triggers `ensure-trigger` installs target **the flow drawer's own name — there is no companion drawer to target instead** (§1):

- **Event-driven**: registers the drawer webhook trigger (`buttons drawer NAME trigger webhook`, already shipped) directly on the flow-kind entity — `SetWebhookTrigger` has no kind restriction today, and `Triggers` is a plain field `MarshalJSON` doesn't touch (only `steps` is stripped, §1), so this needs no code change on the storage side.
- **Polling**: writes a recurring invocation of `buttons drawer NAME press` to whatever the `--on` target's native scheduler is — a user crontab or launchd/systemd unit locally, a generated GitHub Actions workflow with a `schedule:` trigger for `--on github`. This is a file it writes, not a process it starts or supervises — the OS or GitHub's own scheduler owns liveness from here.

**The one real code change this requires, stated plainly**: `cmd/serve.go`'s webhook dispatch currently loads whatever drawer a trigger names and executes it directly (`exec.Execute(ctx, d, ...)`), bypassing `cmd/drawer.go`'s `drawerPress` entirely. Both call sites must route a flow-kind drawer through the same compile function (§1) before executing — `drawerPress` for manual/scheduled presses, the webhook handler for event-driven ones. Skipping this in either place means that path presses a steps-less drawer and silently does nothing; it is the one place "one execution path" (agent-prompt) takes real, deliberate code to guarantee rather than falling out for free.

If neither trigger is registered (a human deletes the cron entry, say), the board simply stops advancing until something presses it again — no crash, no alert, no journal to recover; `flow status` reports "no active trigger found" as a plain drift finding, same as it would report any other misconfiguration.

**No pinning, no `--upgrade` — a deliberate simplification with a real tradeoff.** v1 pinned a board to its `FlowDefinition`'s content hash at activation and refused to run a drifted definition until an explicit `init --upgrade`. Because this design recompiles fresh from whatever `FlowDefinition` is on disk *every single press*, that gate is gone: editing a published flow drawer's definition takes effect on the very next press — webhook, scheduled, or manual — with no confirmation step. Within one press this is safe (the pipeline compiled at the top of that press runs to completion unchanged); across presses, a board's behavior can now shift with no signal beyond re-reading `drawer.json` yourself. This is consistent with "no journal, no pinning" and matches Eric's "select and invoke" framing more literally than a pinned artifact would — but it is a real behavior change from v1, flagged here rather than discovered late. If it proves too loose in practice, the fix is a lightweight guard (e.g. `flow status` surfacing "definition changed since last press"), not resurrecting content-hash pinning and an `--upgrade` verb.

## 7. FlowDefinition schema v3 — unchanged from v1, now understood as compile targets

Everything in v1's §10 (Gates, Roles, stage prompts, capabilities, board config) is kept **as authoring format** — it's still the right vocabulary for a board — but every mechanism description changes from "the engine enforces X" to "the `validate`/`apply` steps enforce X":

- **Gates** (`FlowGate{RequiresHumanApproval, Approvers}`): the `apply` step does not write `status` for an advance out of a gated stage — it sets `flow.pending_approval` instead. `buttons flow approve TASK` presses a small `approve` button that flips the marker the next `apply` step checks; `flow reject` clears it with a reason comment. No operator priority queue is needed — there's no queue to prioritize, just the next press picking it up.
- **Roles** (`FlowRole{SystemPrompt, Model, Capabilities, Heartbeat, Cron, Watches, TimeoutSeconds, Escalation}`): compose into the `perform` step's prompt and the `runtime:"prompt"` button's config at compile time, same composition rule as v1 (board → stage → role, additive).
- **Stage prompts** (`OnFeedback`, `Review`, `Evidence`): `OnFeedback` selects which prompt the `perform` step composes when the triggering event is a comment rather than a stage entry; `Evidence.Required` is a check the `validate` step runs against the turn's output before accepting an advance.
- **Capabilities** (`Skills`, `Tools`): resolve to the `tools` list the compiled `perform` button is allowed to press and the skills it's given — the same registry `requires` mechanism buttons already use.

None of this needs a schema change beyond what v1 already specified; v3's field additions stand as-is.

## 8. CLI surface

Every verb below is confirmed sugar over an existing primitive — stated explicitly so nothing hides a new subsystem:

| Verb | Effect | Implemented as |
|---|---|---|
| `flow init DRAWER [--on local\|github] [--manager REF] [--worker REF] [--no-verify]` | Register the webhook trigger; write the polling schedule (§6.4); press one throwaway task through the pipeline (compiled in memory, same as any press) to prove it. Nothing is generated or persisted. | `drawer trigger webhook`, a written cron/launchd/Actions file, `drawer press` |
| `flow task add\|list\|read\|update\|rm\|claim\|comment` | Task CRUD. | Presses the board's own `task-*` buttons directly — no separate task store |
| `flow status [NAME]` | Counts by status, pending approvals, last press time, trigger drift (webhook missing? schedule file missing?). | `provider-list` + `buttons history <board>` summarized |
| `flow logs [NAME] [--task ID] [--failed]` | Turn records. | `buttons history <board>` filtered |
| `flow approve TASK` / `flow reject TASK` | Commit/refuse a gated transition. | Presses `approve`/`reject` buttons (§7) |
| `flow rm NAME [--purge]` | Remove the webhook trigger and polling schedule; tasks kept unless `--purge`. Nothing else to clean up — there's no generated drawer. | Drawer trigger removal + schedule file removal |

Cut entirely from v1's surface (not deferred — inapplicable): the `sync` machine verb (there's no runner to advance one step; a press already does exactly one pass), `--holder`/`--ttl` on claim (there's no lease). `plan`/`apply`/`verify`/`doctor` as *dedicated verbs* are also cut — but not because convergence itself is rejected (Eric: "even setup and convergence are button functionality"): `ensure-trigger` (§6) already re-registers the board's trigger idempotently on every press, which *is* the convergent behavior those verbs would have provided; it just doesn't need its own CLI surface. `dlq`/`dlq replay` (no dead-letter store — `logs --failed` is just `history` filtered by status).

`buttons drawer NAME press` on a flow drawer now works directly: `drawerPress` compiles the `FlowDefinition` in memory (§1) and executes exactly one pass of the composed pipeline — no redirect, no second entity, whether or not `flow init` has ever run. What `flow init` actually gates is *liveness*, not pressability: without it, the board still presses fine by hand, it just doesn't press itself. `flow init` is what makes that pass repeat on its own.

## 9. Open questions

0. **The discriminated-union constraint is load-bearing and untouched.** `Drawer.MarshalJSON` strips `steps` from any `drawer_kind:"flow"` entity; this PRD relies on that staying true (§1) by never serializing a compiled pipeline — compilation is in-memory, per press, always. If a future change proposes persisting a compiled flow drawer to disk (a cache, a "compiled" sidecar file, anything written back to `drawer_kind:"flow"`'s own JSON), that's exactly the trap this design avoids — treat it as a drawer-schema change and put it on the companion agent-prompt's "where to stop and ask" list, not a silent optimization.
0a. **Compile failures on a live board have no home yet.** Removing content-hash pinning (§6.4) means a bad edit to a published `FlowDefinition` can start failing on the very next webhook or scheduled press, with nothing that caught it at "upgrade time" the way v1's `FLOW_CHANGED` gate did. `validateFlowDefinition` (`internal/drawer/validate.go:334-460`) should run as part of the compile step and produce a clean, surfaced failure — likely `needs_attention` plus a `flow status` line — rather than a raw error the next scheduled press just swallows. Not designed yet; needs one pass during the build.
1. **Claim race window** — §6.3's read-write-wait-reread is best-effort, not a guarantee. Needs one real multi-host test (two presses launched simultaneously against the same GitHub board) before shipping; if the race proves too wide, the fallback is switching the claim marker to a git ref (real CAS, §6.3) rather than issue assignee/label — shorten the wait or add a second re-read first, escalate to the ref-based marker only if that's not enough.
2. **Staleness default** — how long before a claim is considered abandoned. Start at each stage's `TimeoutSeconds`; revisit with usage.
3. **Local provider concurrency across machines** — the keyed queue only serializes one host. If a local board is ever pressed from two machines sharing `~/.buttons` over a network filesystem, flock semantics need verifying (or the claim protocol applies locally too, uniformly, instead of the queue). Decide during the build; simplest fix may be to just always use the claim protocol and treat the queue as a same-host optimization only.
4. **Provider mapping details** (GitHub field vs. label choices, revision derivation for optimistic writes) — v1 §9.3 had this worked out; port those specifics, they don't depend on the runtime question.
5. **Verdict schema** — kept from v1 §7.6 (`advance | retry | hold | delegate | escalate`) — still proposed, not settled; one review round with Bob before hard-coding the `perform`/`validate` button contract.

## 10. Definition of done

In a clean workspace:

```
buttons add @buttonsflow/swe
buttons flow init swe                        # compiles, registers triggers, proves it
buttons flow task add "login page 500s on Safari"
buttons flow task list --filter status=done  # a webhook or scheduled press worked it
```

…and `init --on github` compiles the identical `FlowDefinition` into the same compiled pipeline shape live on GitHub — only the provider buttons the compiler selects differ. Kill any single press mid-turn: the next press (webhook or scheduled) picks the task back up once its claim goes stale, `status` never regresses, and nothing was lost because nothing but the provider was ever the source of truth.
