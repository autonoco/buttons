# Prompt for the coding agent — Buttons Flow

> Paste this together with `buttons-flow-prd.md`. The PRD is the spec. Read it fully before doing anything — including §0, which explains this is a v2 replacement of an earlier, much larger design.

---

You are building **Buttons Flow** — the runtime for `drawer_kind: "flow"` — inside this repository (`github.com/autonoco/buttons`).

## The one thing to internalize

**The definition format already exists and you may not invent another one — and it can never carry `steps` on disk.** `FlowDefinition` (`internal/drawer/entity.go:93-153`) is shipped, validated, content-hashed, and distributed through the registry. `Drawer.MarshalJSON` (`entity.go:72-88`) unconditionally strips `steps` from any `drawer_kind:"flow"` entity — this is an existing, load-bearing invariant, and you may not relax it. The way around it is not a second drawer — it's that the compiled pipeline (PRD §6) **never gets serialized**: write one shared compile function (likely `internal/drawer` — it's still drawer-shaped, not a reason for a new package) that takes a `*Drawer` with `DrawerKind == "flow"` and returns an **in-memory `*Drawer`** with `Steps` populated, and call it from both `cmd/drawer.go`'s `drawerPress` and `cmd/serve.go`'s webhook dispatch, immediately before invoking `internal/drawer/executor.go`. Nothing about that in-memory struct is ever written back to the store, so `MarshalJSON` never runs on it in a way that would strip anything. If your plan persists a compiled pipeline anywhere — a second drawer, a cache file, a sidecar — stop, that's a drawer-schema change in disguise and belongs on the "where to stop and ask" list below. If your plan writes `steps` onto the on-disk `drawer_kind:"flow"` entity itself, or removes/bypasses `MarshalJSON`'s discriminated-union behavior, same thing — stop. Beyond that: if your plan involves a new Go package under `internal/flow/`, a new binary, a daemon, a journal, a lease store, or a scheduler, you have also misread the PRD — stop and say so. There is no new definition format beyond the v3 extensions already in PRD §7 (unchanged from the original design: role registry, gates, capabilities, stage prompts).

## The second thing to internalize

**Statelessness is the product, not a shortcut.** The earlier version of this PRD (superseded, PRD §0) ported 35 durability invariants from a sibling project, `autonoco/turtleflow`, because that project's architecture was a long-running process that could crash mid-work. This design has no such process: every press is a complete pass over provider state, and provider state (a GitHub issue's fields and labels, or a local task file) is the only thing that persists between presses. Do not build a journal "just in case," do not add a lease store, do not add checkpoints. If you find yourself persisting anything buttons doesn't already persist (`pressed/` history) or that the provider doesn't already hold, stop — you've drifted back toward v1's architecture.

## The third thing to internalize

**The claim protocol is PRD §6.3, plus exactly one addition, called out as an addition.** Eric's own protocol is: read assignee → write claim if unassigned → wait ~1s → re-read, back off if lost the race — encapsulated as one `claim` button, or split into a set of buttons if a more granular contract is wanted (Eric offered both; default to one, split only if a real need shows up). PRD §6.3 adds a staleness check on top (a claim older than the stage's `TimeoutSeconds` counts as unclaimed) so a crashed `perform` doesn't orphan a task forever — this is the one deliberate deviation from the reviewer's comments in the whole PRD, and it's flagged as such there; don't add others without flagging them the same way. Single host uses the existing keyed queue (`internal/queue`, `key=task-id, concurrency=1`); multiple hosts use the read-write-wait-reread protocol above, which is deliberately best-effort, not a CAS guarantee — PRD §9 flags it as an open question to pressure-test, with a git-ref-based claim marker (real CAS on GitHub, PRD §6.3) as the named fallback if the race proves too wide, not a conditional write on the issue itself (the Issues REST API has no If-Match) and not a lease store. **Idempotency-key (`--idempotency-key`) is explicitly not this mechanism** — it's a result cache (lookup → execute → store), not an atomic claim. Do not reach for it here.

**The webhook path is a second execution entry point — it does not go through `drawerPress`.** `buttons serve`'s webhook dispatch (`cmd/serve.go`) loads whatever drawer its registered trigger points at and executes it directly, bypassing `cmd/drawer.go`'s `drawerPress` entirely. Both the webhook trigger and the polling schedule (PRD §6.4) target the flow drawer's own name — there's no second drawer to target instead now — which means `cmd/serve.go` must call the same compile function `drawerPress` calls, on the flow-kind drawer it just loaded, before executing it. Miss this in `cmd/serve.go` specifically and a live webhook press silently runs a steps-less drawer and does nothing; this is the one place in the whole build where "one execution path" needs a real, deliberate line of code at two call sites instead of falling out for free from one.

## Engine words never reach the CLI

Same rule as before, narrower list now that there's no engine: internal terms like "provider," "claim marker," "staleness window," "compiled pipeline" stay in code and this document. The CLI surface is exactly PRD §8's table — init, task (add/list/read/update/rm/claim/comment), status, logs, approve, reject, rm. Notably **cut** from any earlier draft you might have seen: `sync` (there's no runner to advance one step — a press already does one full pass), `--holder`/`--ttl` (no lease), `plan`/`apply`/`verify`/`doctor` as dedicated verbs — not because convergence is rejected (Eric explicitly said "even setup and convergence are button functionality"), but because `ensure-trigger`'s idempotent recheck on every press already provides it; it doesn't need its own CLI surface. If you find yourself adding a flag or verb not in PRD §8, stop.

## The recon is done

PRD §2 verifies every claim against this repo's actual code (line numbers included) and against `internal/queue/queue.go` directly. Do not re-derive it; do spot-check any cited line before building on it, and flag drift — the repo moves.

## Rules for the whole build

- **Verify, do not assume.** The README of this repo is known-unreliable (fake trigger syntax, imaginary features); `docs/*.mdx` and the code are honest. Where they disagree, the code wins and you flag it.
- **Translate at press time; the existing executor is still the only thing that runs steps.** Every press of a flow drawer calls the compile function fresh — there is no `init`-time build step, no cached artifact, no "compile once, run many" — compiling a `FlowDefinition` into a `Drawer` struct is cheap (it's a data transform over an already-parsed, already-validated struct, §7's field mappings). What must stay true regardless: `internal/drawer/executor.go` is the only code that ever runs a step. If you catch yourself writing anything that executes a button/switch/for_each outside that executor — even to "optimize" the compiled-and-cached case — stop, that's a second interpreter and the PRD explicitly forbids it. **There is deliberately no `--upgrade` flag and no content-hash pinning** (PRD §6.4) — editing a live flow drawer's definition takes effect on the very next press, by design; do not add a pinning/gate mechanism to be "safer" without flagging it as a real design reversal first (PRD §9, item 0a covers the one gap this opens: surfacing a bad edit that breaks compilation on a live board).
- **Failure never advances state.** `props.status` is written in exactly one place: the `apply` step, on a validated advance verdict — same rule as v1, just enforced by a button instead of an engine. Every other path (a failed turn, a rejected verdict, a lost claim race) leaves `status` untouched and, where PRD §7 calls for it, sets `needs_attention`.
- **Bounded everything that can still grow unbounded.** `pressed/` history retention is whatever buttons already does — don't add a second retention policy. If a provider button accumulates local state (e.g. a local task file), keep it bounded the same way any button would.
- **Merge, never replace.** Task updates are patches against provider state. Unknown props/tags round-trip untouched.
- **Do not extend the query syntax.** `key=value`, comma-separated, AND only — same as action drawers already support via `--filter`.
- **Idempotency is not optional, coordination is not idempotency-key.** Re-running `claim`/`apply` safely (same holder, same outcome) matters because agents retry constantly — but the mechanism is PRD §6.3, not the idempotency cache.
- **One execution path.** A flow board press and a manual `buttons drawer NAME press` are the same code path — there is no separate "tick" or "sync" entry point. If you write logic reachable only from `flow init`'s installed trigger and not from a plain `drawer press`, delete it and fold it into the drawer.
- **Defaults over flags.** The provider defaults to local, `--on github` selects the GitHub buttons; there is no App to provision, no exe VM to provision, no runtime choice for the agent turn beyond the existing `runtime: "prompt"` button kind. A flag is an escape hatch, not a decision the happy path asks the user to make.
- **House style.** Cobra NAME-first verb dispatch per `cmd/drawer.go`; stdlib `testing` only; integration tests via the real-binary harness (`test/integration/helpers_test.go`); `go generate` keeps docs/schemas synced.

## Build order

There are no phases in the durability-invariant sense v1 had (there's no invariant list left to satisfy), but there is a natural build order:

1. The compiler: `FlowDefinition` → in-memory composed `Drawer` (PRD §6's pipeline), covering local provider only. Wire it into `drawerPress` first; prove `buttons drawer NAME press` runs the pipeline with no `flow init` involved at all.
2. The claim protocol (PRD §6.3) as a documented button pattern, tested with two concurrent local presses.
3. Wire the same compile call into `cmd/serve.go`'s webhook dispatch (the one non-obvious call site, PRD §6.4). Then `flow init --on local`: register the webhook trigger, write the polling schedule (crontab/launchd/systemd — pick the platform-native one, verify it actually fires), prove with a throwaway task.
4. Gates, roles, stage prompts, capabilities (PRD §7) — wire into the compiled `perform`/`validate`/`apply` steps.
5. The GitHub provider buttons + `--on github` (compile the same pipeline against GitHub buttons; write the Actions `schedule:` workflow instead of a local cron entry).
6. `flow status`/`flow logs`/`flow approve`/`flow reject` as thin CLI sugar (PRD §8).

Nothing is done until the acceptance items implied by PRD §3 and §10 pass, **demonstrated, not asserted**: run the commands, paste the output, including the "kill a press mid-turn, the next one recovers via claim staleness" case.

## Where to stop and ask

Surface these rather than deciding them:

- The turn I/O contract (verdict schema, PRD §9.5) — proposed, not settled; get Bob's review before hard-coding the `perform`/`validate` button contract.
- The claim race window (PRD §9.1) — if a real multi-host test shows the race is wider than "surprising, rare," that's a design conversation, not a place to quietly bolt on a lease store.
- Anything that would change the action-drawer executor itself, the registry package format, or the v1/v2 drawer schema beyond the v3 extensions in PRD §7.
- Any new user-facing verb, flag, or word not already in PRD §8.
- GitHub API/field behavior that contradicts PRD §7's mapping notes — verify against a real org early.
- Any place you find yourself wanting a durable process, a journal, or a lease — that's a sign the stateless design doesn't cover a case PRD §9 didn't anticipate. Say so; don't build the workaround.

## Definition of done

In a clean workspace:

```
buttons add @buttonsflow/swe
buttons flow init swe                        # compiles, registers triggers, proves it
buttons flow task add "login page 500s on Safari"
buttons flow task list --filter status=done  # a webhook or scheduled press worked it
```

…and `init --on github` makes the same board live on GitHub, running the identical drawer against GitHub-backed provider buttons instead — no App, no VM, no daemon, in either case.

Build it all.
