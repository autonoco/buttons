# Buttons Flow example — keeping this repo's own docs honest

**Status:** Illustrative, tied to `buttons-flow-prd.md` (v2). Sections 1–3 below (authoring the board and its buttons) use CLI commands that exist today and can be run as-is — this is the concrete demonstration of the PRD's central claim, that the ingredients are already in the codebase. Sections 4–5 (`buttons flow init`, `flow task`, `flow status`, `flow approve`) depict the PRD's proposed CLI surface (§8) and aren't runnable until that ships. Every point where "real today" ends and "proposed" begins is called out explicitly, the same way `examples/apify-webhook-scrape.md` flags `on_failure.*` as JSON-edit-only until the setter lands.

## The task domain

This repo already knows its own docs go stale. `CLAUDE.md` says it outright: "The README of this repo is known-unreliable (fake trigger syntax, imaginary features)." That's the running example here — a flow board that finds two different kinds of doc problem and drives each through triage → draft → human review → merged, continuously, as the repo moves:

- **stale** — a doc claims something the code no longer backs up (a removed flag, a renamed verb). Needs correcting.
- **gap** — the code has a real, shipped capability no doc mentions at all (a new command, a new flag nobody wrote up). Needs writing, not fixing — there's nothing wrong to correct, just something missing.

Same pipeline either way; only the `draft` stage's approach differs (correct vs. author from scratch), and the finding carries which kind it is as a tag (`type:stale` / `type:gap`) — a tag, not a prop, since it's not something either stage's transitions branch on. Deliberately not called `type:drift`: PRD §8's `flow status` already uses "trigger drift" for a completely different thing — whether the board's own webhook/cron registration is still in place — and reusing the word for "this doc's content is wrong" would make the two impossible to tell apart on sight. GitHub-hosted, since a doc board with no visible issue trail would be a strange thing to trust.

## 1. The board's stages

| Stage | Does | Transitions to |
|---|---|---|
| `triage` (initial) | Re-checks the finding against current code — for `type:stale`, is the claim still wrong; for `type:gap`, is the capability still undocumented (the repo moves either way). Decides: real, or false positive/already covered. | `draft`, `needs-design`, `done` |
| `draft` | For `type:stale`, corrects the claim; for `type:gap`, writes the missing section from scratch. Opens a PR either way, posts the link as a comment. | `review` |
| `needs-design` | Escalation target for ambiguous cases (two docs disagree on the truth; where a new section should live isn't obvious). Parks with `needs_attention` for a human to decide, then hands back. | `draft`, `done` |
| `review` | Gated — a human approves or requests changes on the PR. | `done`, `draft` |
| `done` | Terminal. Issue closed, PR merged (or the false-positive/already-covered no-op case). | — |

## 2. Author the flow drawer

All real, runnable today:

```sh
buttons drawer create stale-docs --kind flow

buttons drawer stale-docs stage add triage --title "Triage" \
  --system-prompt "A finding is either type:stale (a doc claims something the buttons CLI no longer does) or type:gap (a shipped command/flag that no doc mentions). Re-check it against the current code (cmd/, internal/). Decide: still real (advance to draft), was already fixed or already documented (advance to done), or genuinely ambiguous — two docs disagree, or where new content belongs isn't obvious (advance to needs-design)."

buttons drawer stale-docs stage add draft --title "Draft" \
  --system-prompt "If type:stale, correct the doc claim to match current code. If type:gap, write the missing section — find the nearest existing doc it belongs in (README.md, CLAUDE.md, docs/*.mdx) rather than inventing a new file. Open a PR against the affected file(s) with a tight, factual diff — no unrelated cleanup. Summarize what changed and why in the PR body. Advance to review once the PR is open."

buttons drawer stale-docs stage add needs-design --title "Needs a human call" \
  --system-prompt "Summarize the ambiguity for a human and hold — do not guess which doc is authoritative, or where undocumented behavior should be written up."

buttons drawer stale-docs stage add review --title "Human review"

buttons drawer stale-docs stage add done --title "Done"

buttons drawer stale-docs set flow.initial_stage=triage

buttons drawer stale-docs set 'flow.stages.triage.transitions=["draft","needs-design","done"]'
buttons drawer stale-docs set 'flow.stages.draft.transitions=["review"]'
buttons drawer stale-docs set 'flow.stages.needs-design.transitions=["draft","done"]'
buttons drawer stale-docs set 'flow.stages.review.transitions=["done","draft"]'

buttons drawer stale-docs set flow.stages.draft.timeout_seconds=1800
```

`review` never gets a `worker.agent` set — `FlowStage.Worker` is optional (`omitempty`, nil unless assigned), so it's a human gate by simple omission, not by any special flag. `validateFlowDefinition` (`internal/drawer/validate.go:404`) enforces the agent-reference format on any stage that *does* set one (`activation.manager|activation.worker|agent:<tenant>:<name>` only — no free-form sentinel like "none" would pass), so there's no invented convention hiding here. That's the piece that isn't fully wired up yet:

**Not real yet — v3 extension.** `FlowGate{RequiresHumanApproval, Approvers}` (PRD §10.1) doesn't exist in `internal/drawer/entity.go` today, so `set flow.stages.review.gate.*` returns `FLOW_FIELD_UNKNOWN`. Until it ships, express the same intent with a plain prop the `apply` step (§3 below) checks by hand:

```sh
buttons drawer stale-docs set flow.stages.review.completion.requires_summary=true
```

...and treat "task has a `pr_url` prop but no `approved` prop" as the gate condition inside `stale-docs-apply`'s logic (§3) — a real, working stand-in for the proposed `FlowGate`, just enforced in a button instead of a schema field. Same story for named roles (`FlowRole`, PRD §10.2): this board is small enough that `stage.worker.agent` / `stage.manager.agent` (both real, shown above) are enough; a bigger board would want the role registry once it ships.

## 3. The buttons behind the pipeline

Three are specific to this board's domain and worth showing in full. The rest (`provider-list`, `perform`, `validate`, `apply`, `ensure-trigger`) are the generic PRD §6 roles — shown as a table, since their shape is the same for any GitHub-backed flow board, not particular to doc-staleness.

### `stale-docs-scan` — the intake

Not part of the per-task pipeline (§6's `provider-list → actionable → claim → perform → validate → apply` loop runs over *existing* tasks) — this is what creates new ones. It runs on its own schedule (§4) and files a task per stale/gap finding, deduped against what's already open.

```sh
buttons create stale-docs-scan \
  --runtime shell \
  --arg repo:string:required \
  --code '
set -euo pipefail
existing=$(gh issue list --repo "$BUTTONS_ARG_REPO" --label flow:stale-docs --state open --json title --jq ".[].title")

# Pass 1 — stale: pull every backtick-quoted `buttons ...` invocation
# out of the docs, check the command still resolves. Flags a claim
# that used to be true and no longer is.
grep -rohE "\`buttons [a-z][a-z0-9 -]*\`" README.md CLAUDE.md docs/*.mdx examples/*.md 2>/dev/null \
  | tr -d "\`" | sort -u | while read -r claim; do
    cmd=$(echo "$claim" | awk "{print \$2}")
    if ! ./buttons "$cmd" --help >/dev/null 2>&1; then
      title="stale doc: \`$claim\` no longer resolves"
      echo "$existing" | grep -qF "$title" && continue
      gh issue create --repo "$BUTTONS_ARG_REPO" \
        --title "$title" \
        --body "Found by stale-docs-scan (stale pass). Claim: \`$claim\`. \`./buttons $cmd --help\` failed." \
        --label flow:stale-docs --label "type:stale" --label "status:triage"
    fi
  done

# Pass 2 — gap: pull every subcommand verb out of cmd/*.go'"'"'s cobra
# `Use:` strings, check whether any doc mentions it at all. Flags a
# real, shipped verb that was never written up.
grep -rhoE "Use:\s*\"[a-z][a-z0-9-]*" cmd/*.go 2>/dev/null \
  | sed -E "s/Use:[[:space:]]*\"//" | sort -u | while read -r verb; do
    [ "$verb" = "help" ] && continue
    if ! grep -rq "$verb" README.md CLAUDE.md docs/*.mdx examples/*.md 2>/dev/null; then
      title="undocumented: \`$verb\` has no mention in any doc"
      echo "$existing" | grep -qF "$title" && continue
      gh issue create --repo "$BUTTONS_ARG_REPO" \
        --title "$title" \
        --body "Found by stale-docs-scan (gap pass). Verb \`$verb\` exists in cmd/*.go but no doc file mentions it." \
        --label flow:stale-docs --label "type:gap" --label "status:triage"
    fi
  done
' \
  --description "Diff doc-quoted CLI commands against the real CLI (stale) and real verbs against the docs (gap); file a task per finding" \
  --timeout 120
```

Real and runnable — point it at any repo, it files real GitHub issues, both passes. The gap pass is a blunt heuristic (any doc mentioning the verb string anywhere counts as "documented," even in an unrelated sentence) — good enough to file a candidate for `triage` to actually judge, not a claim of precision. What it *can't* do yet is call `buttons flow task add` directly (that verb doesn't exist), so today it talks to the provider (`gh issue create`) the same way `stale-docs-provider-list` and `stale-docs-claim` do below — which is itself the point: task creation, listing, and claiming are all just buttons wrapping `gh`, nothing engine-shaped about any of them.

### `stale-docs-provider-list` — read current state

```sh
buttons create stale-docs-provider-list \
  --runtime shell \
  --arg repo:string:required \
  --code '
gh issue list --repo "$BUTTONS_ARG_REPO" --label flow:stale-docs --state open \
  --json number,title,body,labels,assignees \
  --jq "[.[] | {id: (.number|tostring), title, body, props: {status: ([.labels[].name | select(startswith(\"status:\"))][0] // \"triage\" | sub(\"status:\";\"\"))}, tags: [.labels[].name], claimed_by: (.assignees[0].login // \"\")}]"
' \
  --description "List open stale-docs tasks in provider Task shape" \
  --timeout 30
```

`status:<stage>` labels are this board's `props.status` mapping — one reasonable choice; PRD §9 (open question 4) leaves the exact GitHub field/label convention unsettled, this is what "pick one" looks like in practice.

### `stale-docs-claim` — Eric's exact protocol

```sh
buttons create stale-docs-claim \
  --runtime shell \
  --arg repo:string:required \
  --arg issue:string:required \
  --arg holder:string:required \
  --code '
set -euo pipefail
assignee=$(gh issue view "$BUTTONS_ARG_ISSUE" --repo "$BUTTONS_ARG_REPO" --json assignees --jq ".assignees[0].login // empty")
if [ -n "$assignee" ]; then
  echo "{\"claimed\": false, \"reason\": \"already assigned to $assignee\"}"
  exit 0
fi
gh issue edit "$BUTTONS_ARG_ISSUE" --repo "$BUTTONS_ARG_REPO" \
  --add-assignee "$BUTTONS_ARG_HOLDER" --add-label "Agent Claimed"
sleep 1
now=$(gh issue view "$BUTTONS_ARG_ISSUE" --repo "$BUTTONS_ARG_REPO" --json assignees --jq ".assignees[0].login // empty")
if [ "$now" != "$BUTTONS_ARG_HOLDER" ]; then
  echo "{\"claimed\": false, \"reason\": \"lost the race to $now\"}"
  exit 0
fi
echo "{\"claimed\": true}"
' \
  --description "Read assignee, claim if unassigned, wait 1s, re-check (PRD §6.3)" \
  --timeout 15
```

Read → claim-if-unassigned → wait ~1s → re-check → back off if lost the race. Nothing else. The staleness half of §6.3 (a claim older than the stage's `timeout_seconds` counts as unclaimed again) lives in `stale-docs-actionable`'s CEL, not here — it's a read-time filter, not part of the claim write.

### The generic roles

| Button | Role (PRD §6) | Shape |
|---|---|---|
| `stale-docs-actionable` | not a button — a `switch` step in the compiled pipeline: `status in stage.transitions-reachable AND (unclaimed OR claim older than stage.timeout_seconds)` | CEL, inline |
| `stale-docs-perform` | `perform` | `runtime:"prompt"`; system prompt = board prompt (none here) + the claimed task's stage `system_prompt` (§2) — one button, reused across all five stages, prompt swapped per stage at compile time |
| `stale-docs-validate` | `validate` | shell/JSON-parse button; checks the manager's verdict `to_stage` is in `flow.stages.<current>.transitions` before letting an `advance` through |
| `stale-docs-apply` | `apply` | `gh issue edit --remove-label "status:<old>" --add-label "status:<new>"` + `gh issue comment` with the verdict summary; also where the `review` gate stand-in lives (§2) — refuses to advance `review → done` unless an `approved` comment/label is present |
| `stale-docs-ensure-trigger` | `ensure-trigger` | idempotent — checks/(re)installs the webhook trigger and the Actions `schedule:` workflow (§4); a no-op after the first run |

## 4. Go live

**Proposed CLI (PRD §8) — not runnable until Buttons Flow ships:**

```sh
buttons flow init stale-docs --on github
```

What this does, per the PRD: registers a webhook trigger on `stale-docs` itself for issue-comment/label events (`buttons drawer stale-docs trigger webhook /stale-docs` under the hood — that part's real today), writes a GitHub Actions workflow with a `schedule:` cron entry that runs `buttons drawer stale-docs press` every 15 minutes (and `stale-docs-scan` on its own slower cadence, say daily), and presses one throwaway task through the whole pipeline to prove it end to end. No App, no VM, no daemon — the Actions runner and the webhook listener are the only two things that ever invoke anything, and both already exist as mechanisms (`cmd/serve.go`'s webhook dispatch, GitHub's own cron).

Because compilation is live and in-memory (PRD §1), editing any of the `set flow.*` calls from §2 — say, tightening the `draft` stage's timeout — takes effect on the very next press. No `--upgrade`, nothing to re-run.

## 5. Watch it work

**Proposed transcript**, illustrating the shape of PRD §8's verbs:

```
$ buttons flow task list --filter status=triage
{"ok":true,"data":{"tasks":[
  {"id":"142","title":"stale doc: `buttons trigger add --watch` no longer resolves","tags":["type:stale"],"props":{"status":"triage"}},
  {"id":"144","title":"undocumented: buttons trigger add --cron has no mention in CLAUDE.md's Stage 2 plumbing section","tags":["type:gap"],"props":{"status":"triage"}}
]}}

# (a scheduled press claims #142, triages it as still stale, opens PR #143 correcting docs/*.mdx, advances to review)
# (another claims #144, triages it as a real gap, opens PR #145 adding the missing section, advances to review)

$ buttons flow status stale-docs
{"ok":true,"data":{"by_status":{"triage":0,"draft":0,"review":2,"done":14},
  "pending_approvals":2,"last_press":"2026-08-02T14:03:11Z","trigger_drift":null}}

$ gh pr view 143 --repo autonoco/buttons
  docs: buttons trigger add no longer takes --watch as a bare flag
  ...

$ gh pr view 145 --repo autonoco/buttons
  docs: document buttons trigger add --cron in CLAUDE.md
  ...

$ buttons flow approve 142
{"ok":true,"data":{"task":"142","committed":true}}

$ buttons flow approve 144
{"ok":true,"data":{"task":"144","committed":true}}

# apply merges PR #143 and #145, sets status:done on both, closes #142 and #144

$ buttons flow task list --filter status=done --limit 2
{"ok":true,"data":{"tasks":[{"id":"142","props":{"status":"done"}},{"id":"144","props":{"status":"done"}}]}}
```

## Notes

- **The claim race, demonstrated concretely.** Two scheduled presses landing in the same 15-minute window both see issue #142 unassigned, both write themselves as assignee — one wins, the other's `stale-docs-claim` re-check (§3) sees the other's login and backs off to the next task. No lock file, no lease, just `gh issue edit` twice and a `sleep 1`.
- **The staleness window, and why it's there.** If a claiming press crashes mid-`draft` (agent process killed, box rebooted), issue #142 stays assigned forever under Eric's protocol alone — nothing re-examines an already-claimed issue. `stale-docs-actionable`'s CEL check (a claim older than `draft`'s `timeout_seconds` counts as unclaimed) is the one deliberate addition PRD §6.3 flags beyond what Eric specified, and this is the failure mode it exists for.
- **`type:stale` vs. `type:gap`, why both matter and why neither is called "drift."** A `fix`-only board only ever notices something that used to be true and no longer is; it would never catch `buttons trigger add --cron` shipping with zero documentation, because nothing about that is *wrong* — it's just missing. Same pipeline, same claim protocol, same gate; only `triage`'s and `draft`'s system prompts branch on which kind it is. And neither is spelled `type:drift`, because PRD §8 already owns that word for something else entirely (trigger registration going missing) — the transcript above carries both `type:stale`/`type:gap` tags and an unrelated `trigger_drift` status field side by side on purpose, to show they don't collide.
- **What's real vs. proposed, one more time.** Everything in §2 and §3 runs against the actual CLI in this repo today — creating five stages, wiring transitions, writing three domain buttons — and would genuinely find and start fixing real stale docs (and drafting real missing ones) if pressed by hand right now (minus the `flow.stages.review.gate` line, which needs the v3 schema extension first). §4 and §5 are what pressing it *automatically, forever* looks like once the PRD ships.
