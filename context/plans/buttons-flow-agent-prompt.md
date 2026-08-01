# Prompt for the coding agent — Buttons Flow

> Paste this together with `buttons-flow-prd.md`. The PRD is the spec. Read it fully before doing anything.

---

You are building **Buttons Flow** — the runtime for `drawer_kind: "flow"` — inside this repository (`github.com/autonoco/buttons`).

## The one thing to internalize

**The definition format already exists and you may not invent another one.** `FlowDefinition` (`internal/drawer/entity.go:93-153`) is shipped, validated, content-hashed, and distributed through the registry. Pressing a flow drawer already tells users a "Buttons Flow runtime" will run it (`cmd/drawer.go:475-480`). You are building that runtime: `cmd/flow.go` + `internal/flow/`. If your plan involves a new definition schema, a new binary, or changes to the action-drawer executor, you have misread the PRD — stop and say so. The only schema changes permitted are the versioned v3 extensions in PRD §10: the role registry, gates, capabilities, stage prompts (on_feedback, review/evidence), and board config (ticket_prompt, composition, policies). No permission model and no sandbox settings exist — agents run full-access by decision; do not add either.

## The second thing to internalize

**The durability invariants are the product.** Appendix A of the PRD lists 35 of them, each extracted from `autonoco/turtleflow` (a sibling checkout is your reference — read its code when the PRD cites it; do not import it). A feature that works until you `kill -9` it is not done. The flagship acceptance test — kill the runner mid-turn, restart, watch the run resume exactly once with status unchanged — is the bar every mechanism must clear.

## The third thing to internalize

**Engine words never reach the CLI.** The full banned list is PRD §1's vocabulary rule (tick, journal, lease, activation, backend, ingress, pump, sweep, tenant, delivery, holder, TTL, revision, serve, run loop, attached/detached) — these live in the code and the PRD, nowhere a user sees. The surface is plain words — init, task, status, logs, approve; reject, cancel, rm; machine verb sync — and PRD §8 is exhaustive, including the deliberately-absent list. A board has no lifecycle — no start, no stop, no run: `init` makes it live and installs the runner that keeps it live. There is no retry: recovery is conversation (PRD §7.8) — failures land as comments on the task, and a human's reply is the turn that re-dispatches. And there is no pause: taking over a task is a comment plus `task claim`, which holds agents off via the same lease workers use (PRD §7.3). If you find yourself adding a flag or verb not in §8's table, stop — either it's a default in disguise or it belongs in v1.1.

## The recon is done.

What a recon phase would produce is already in the PRD (§2 + Appendix A), verified against both codebases. Do not re-derive it; do spot-check any cited line before building on it, and flag drift — the repos move.

## Rules for the whole build

- **Verify, do not assume.** The README of this repo is known-unreliable (fake trigger syntax, imaginary features); `docs/*.mdx` and the code are honest. Where they disagree, the code wins and you flag it.
- **Journal before effect.** No visible side effect without a durable record that will replay or pin-as-failed it. O_EXCL create is the claim; tmp+rename 0600 is the write.
- **Failure never advances state along the pipeline.** `props.status` is written in exactly one place: the validated-advance path — with one deliberate, configured exception: role escalation may move a task to its escalation stage (PRD §10.2). Every other error path pins the task where it is and sets `needs_attention`.
- **Identity is derived, then asserted.** Session and VM names are pure functions of (flow, task, role, …). Re-derive and compare before every operation. Never store a name you could derive.
- **Bounded everything.** Retries (3), WorkLog (20/14d), journal (2000/14d), runs (500/30d), result payloads (128 KiB). If you add a store, add its bound in the same commit. Pruning is automatic — never a verb the user must remember.
- **Merge, never replace.** Task updates are patches. Unknown props and tags round-trip untouched — this is what makes the system general.
- **Do not extend the query syntax.** `key=value`, comma-separated, AND only. Wanting more means the flow is wrong, not the syntax.
- **Idempotency is not optional.** Re-running update/rm/claim (same holder) must be safe. Agents retry constantly.
- **One execution path.** Internally, the unit of convergence is a tick; the runner is a loop over ticks plus the webhook mount, and `buttons flow sync` (the documented machine verb) performs one tick. If you write logic reachable from the runner but not from a single tick, delete it and move it into the tick.
- **Defaults over flags.** The agent runtime is acpx, installed by init — no runtime choice exists; the store defaults to local and infers github; verification runs on first converge unless `--no-verify`; the step budget comes from the invocation mode. Setup is `init` and only `init` — consent-gated, resumable, idempotent; re-running it converges. The runner never provisions. A flag is an escape hatch, not a decision the happy path asks the user to make.
- **House style.** Cobra NAME-first verb dispatch per `cmd/drawer.go`; stdlib `testing` only; integration tests via the real-binary harness (`test/integration/helpers_test.go`); `go generate` keeps docs/schemas synced. Add `"flow"` to the `logs`-rewrite denylist at `cmd/root.go:74-86` in your first PR — `buttons flow logs` misroutes without it.
- **Write the conformance suite before the second store exists.** The local store and the GitHub store must pass the same contract tests; that suite is the artifact that makes the swappable-store thesis real.

## Build order

There are no phases. The deliverable is the whole of PRD §11 at once — sequence your own work however you like, but nothing is done until **all** of its acceptance list passes, **demonstrated, not asserted**: run the commands, paste the output, the kill -9 flagship included.

## Where to stop and ask

Surface these rather than deciding them:

- The turn I/O contract (PRD §12.1) — the verdict schema is proposed, not settled; get Bob's review before hard-coding worker output shapes.
- Anything that would change the action-drawer executor, the registry package format, or the v1/v2 drawer schema beyond the v3 extensions specified in PRD §10.
- Manager.HeartbeatSeconds vs stage heartbeat semantics when both are set (PRD §12.2).
- Any place a FlowDefinition field turns out to be insufficient at runtime — propose a versioned extension, do not work around it with hidden state.
- Any new user-facing verb, flag, or word not already in PRD §8.
- GitHub API behavior that contradicts the PRD's mapping (org issue fields are a very new API; verify against a real org early).

## Definition of done

In a clean workspace:

```
buttons add @buttonsflow/swe
buttons flow init swe                        # provisions, installs the runner, proves it — the board is live
buttons flow task add "login page 500s on Safari"
buttons flow task list --filter status=done  # agents worked it; nobody ran anything
```

…and `init --on github` makes the same board live on GitHub + exe, running the identical drawer unmodified, `kill -9` of the runner at any point included.

Build it all.
