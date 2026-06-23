# Buttons as the skill runtime — distribution, versioning & OTA

Status: proposal for review.

> **Orientation.** Authored from the perspective of the first real consumer — the `bobak-executive-desk`
> "autono" buttons (~124 of them). Throughout this doc **"this repo" / "our buttons" = that consumer**,
> and **"the buttons repo" = `autonoco/buttons`**. It supersedes an earlier `SKILL.md`-file distribution
> approach (skills + symlinks + the Claude Code marketplace), ruled out because skills are model-invoked
> and don't reliably trigger, symlinks break OpenClaw, and the marketplace is flaky — so the plan pivots
> to **Buttons** (`autonoco/buttons`, Apache-2.0) as the distribution *and* execution substrate.
>
> See also the epic-level architecture mapping: autonoco/autono#251.

Grounded in a full read of the Buttons Go source, its release pipeline, and the consumer's button trees.

## TL;DR — the decision

**Pivot from skills to Buttons.** It wins on the three things skills fail at, *by construction*:

- **Reliability** — a button is **executed** (`buttons press`), not **inferred**. This kills "I always
  have to tell the agent to use the skill." (And the root‑cause fix for discovery is to expose buttons
  as **MCP tools** — see §3 — so the agent sees them in its tool list without being told.)
- **Ownership** — it's your OSS runtime; you control the execution model, the registry, the update path.
- **Your two hard constraints are satisfied natively:** `buttons store install` **copies** button
  folders into `~/.buttons/buttons/` (real files, **no symlinks**), and OTA runs through **Buttons' own
  self‑updater** (**not** the Claude marketplace).

**What already exists vs. what you build:**

| | Status |
|---|---|
| Deterministic `buttons press`, typed args, batteries (secrets) | ✅ built |
| **Binary self‑update** (`buttons update`: GitHub Releases → SHA256 → atomic swap, Homebrew‑aware, `--check`/`--json`) | ✅ built, robust — *manual, binary‑only* |
| **Release pipeline** (goreleaser auto‑tags next minor on every push to main → npm `@autono/buttons` via OIDC, Homebrew cask `autonoco/homebrew-tap`, GitHub Releases, ghcr Docker) | ✅ built, excellent |
| Cross‑agent **instruction injection** (`agentskill.Install` writes `CLAUDE.md`/`AGENTS.md`/`.cursor` rules between `BUTTONS:START/END` markers, idempotent) | ✅ built |
| Deterministic catalog (`buttons summary --json` / `list --json`) | ✅ built |
| **`buttons store`** (search/install/import/publish) | ❌ **pure stub** — `// TODO Phase 5.1 (#274)` → "not yet implemented" |
| Button **version / tags / pack manifest / provenance** | ❌ none (only an on‑disk `schema_version`) |
| **Content auto‑update / background updater** | ❌ none (no daemon, no on‑run check) |
| Buttons‑as‑**MCP** meta‑tools | ❌ not built (CLAUDE.md "Phase 2") |

So the work is **two tracks**: **(A)** finish Buttons into a distribution runtime (mostly the already‑
planned `#274` plus small Go additions), and **(B)** make our autono buttons actually *distributable* —
which is the bigger, less obvious half, because **our buttons are not self‑contained.**

---

## 1. The uncomfortable core finding: our buttons are coupled to this repo

A button folder (`button.json` + `main.sh|.py` + `AGENT.md`) is already the right *shape* — 124 of them
exist. But our buttons are **thin shells over this TypeScript repo + the local brain/lake**, coupled in
**four** ways that all break the moment a button is copied to `~/.buttons/buttons/` on another machine:

1. **TS‑CLI coupling — 54 buttons run `bun src/cli.ts …`** (cwd = repo root). With no repo present they
   can't run at all.
2. **Repo‑root coupling — ~88 buttons call `git rev-parse --show-toplevel`** to find paths.
3. **Peer‑button coupling via cwd —** `cal-sync`/`meet-sync` reach `pd-proxy-request` by setting
   `cwd=.claude/skills/autono-connectors`. After a flat install there's no such tree → the dependency
   call resolves to nothing.
4. **Secret coupling —** `pd-proxy-request` reads a repo `.env` by walking up from cwd + `PIPEDREAM_*`
   process env, **not** Buttons' `batteries`. No repo → no `.env` → missing creds.

Plus **flat‑namespace collisions:** install target is `<DataDir>/buttons/<name>` (flat). Our trees
already collide — `cal-list`/`cal-sync` exist in **both** `autono-cal` and `autono-gworkspace`;
`setup-doctor` in **5** trees; `meetings-*` in 3–5. Installing them all clobbers.

**Implication:** "just publish the button folders" works only for the *self‑contained* buttons (the
`pd-*` connector toolkit, brain queries). The sync/graph buttons need decoupling first. **This gates v1
scope** — publish the self‑contained buttons first; ship the TS‑heavy ones once the desk CLI is a
distributable artifact (§4, Track B).

---

## 2. What you already own (reuse, don't rebuild)

- **Binary self‑update** (`cmd/update.go`): GitHub Releases → download `buttons_<ver>_<os>_<arch>.tar.gz`
  + `checksums.txt` → SHA256 verify → atomic temp‑file+rename swap (with `.old` rollback) → Homebrew‑
  managed detection. *This exact code path is the template for content updates.* (One bug to fix: it
  uses `browser_download_url`, which 404s on a **private** repo; `install.sh` already does the
  asset‑by‑ID API route — align them so background updates don't silently fail.)
- **Release pipeline:** `.github/workflows/auto-release.yml` auto‑tags the next minor on every push to
  main and runs `goreleaser release`; `.goreleaser.yaml` produces the tarballs+checksums, the Homebrew
  cask (pushed to `autonoco/homebrew-tap`), and the Docker images; `scripts/publish-npm.mjs` publishes
  `@autono/buttons` with OIDC trusted publishing. **The brew/npm pipeline you referenced is real and
  battle‑tested — and it's the model to mirror for a content pipeline.**
- **`batteries`** (`BUTTONS_BAT_<KEY>` injected per press, redacted in `list`) — the secret mechanism a
  pack should declare requirements against (ship KEY *names*, never values).
- **`agentskill.Install`** — already writes the cross‑agent instruction block into `CLAUDE.md` /
  `AGENTS.md` / `.cursor` / `.clinerules` / copilot between idempotent `BUTTONS:START/END` markers. The
  "tell every agent these exist" half of cross‑agent distribution is **built**.

---

## 3. The real fix for "skills don't trigger": buttons as MCP tools

The user's core pain is *discovery/triggering*. Buttons already gives a **deterministic catalog**
(`buttons summary --json`) — strictly better than hoping the model loads a `SKILL.md`. But the agent
still has to know to run it. The root‑cause fix (already on the Buttons roadmap as "Phase 2"):

> **Build the Buttons MCP meta‑tool server** (`buttons_list` / `buttons_press` / `buttons_inspect`).
> Then every button is a **native, model‑invocable tool in the agent's tool list** — discovered and
> called through normal tool‑use, with typed args and descriptions, **with no skill‑auto‑load step and
> no "shell out to `buttons list` first."**

This is why Buttons beats skills *and* beats shelling‑out for reliability. Pair it with two cheap schema
additions — **`tags`** (group/filter: "all finance buttons") and **`buttons search <intent>`** (lexical
now; AutonoDB `@near` later, since you own the brain) — and discovery becomes robust instead of a long
flat list the model has to scan.

**Principle — CLI/MCP parity (hard constraint).** MCP is a *thin adapter over the same engine*, never a
separate or required path. The CLI stays canonical: `buttons_list`/`buttons_press`/`buttons_inspect`
call the exact code `buttons list`/`press`/`detail` do. The agent may discover via MCP and then run via
`buttons press` (often easier for chaining/args), or do everything on the CLI — **zero feature gap
either way; nothing is ever MCP-only or CLI-only.** The MCP server is a presentation layer over the
engine, so parity is structural, not something to maintain by hand.

**The MCP server is a runtime/execution surface (not the control plane)**, and as a runtime adapter it owes
a few architecture-required behaviors — today mostly gaps to close, named honestly:
- **Identity on press.** A `buttons_press` should carry actor / principal / runtime / session from the
  runtime's identity (not model-asserted text), so brain-touching buttons resolve the *invoking principal's*
  partition and every press emits an audit event. v1: a single local principal is implied.
- **Reduced-mode preflight.** A button whose required secret bindings are unmet is mounted **visible but
  flagged unrunnable** (or omitted) — never silently mountable-then-failing.
- **Mutation gate.** Read vs. mutating buttons are distinguished (`mutates`); a mutating press surfaces a
  preview/confirm at the adapter boundary so model-invoked mutations aren't auto-executed; read buttons run
  directly. Auto-update-on-by-default applies to **content updates**, never to silently broadening what a
  model may auto-press.

---

## 4. The two build tracks

### Track A — finish Buttons into a distribution runtime (the `autonoco/buttons` repo)

All small, additive Go changes in a repo you own; most is the already‑scoped `#274`.

1. **Schema (the load‑bearing unit).** Two tiers:
   - **Shipped (#186):** `Version`, `Tags`, `Requires` (peer buttons), `RequiresBatteries` (secret *names*),
     `Source`, `ContentHash` on `button.json`; a `pack.json` (`{ name, version, description, buttons[],
     dependencies[], requires_batteries[] }`). All `omitempty`.
   - **Architecture-aligned target (add as the store/IAM layers land):** per button — `Mutates bool` +
     `RiskLevel` (read vs. mutation; gates the call-time confirm), **`Capabilities []string`** (stable
     capability IDs like `calendar.free_busy.read`, *decoupled from pack version* — the join key to IAM
     grant/policy/audit; a version bump must not silently change them), `Classification` + `SourcePolicy`
     (most-restrictive-wins; an install/press must not loosen a stricter source), `AuthMode`
     (`local-native|brokered|service-side|governed-platform|static-local`), `AllowedEnvironments`/
     `AllowedRuntimes`, `TokenAudience`. Upgrade `RequiresBatteries` from a name list to structured **secret
     bindings** `{ logical_name, inject_as{type}, provider_ref?, environments[], allowed_runtimes[],
     rotation_hint }` — today only `inject_as=env` is implemented (brokered / service-side injection is a
     named gap, §9). **`Tags` (discovery) and `Capabilities`/`Mutates` (policy) are different axes — don't
     conflate them.**
2. **Implement `buttons store`** (`cmd/store.go`, today the stub): `store add <git-url>` / `store
   install <pack>` / `store update [--all]` — backed by `internal/store` that clones/pulls a pinned ref
   and **copies** each button folder into `ButtonsDir()`, stamping `Source`+`Version` into `button.json`.
   **Reuse `update.go`'s fetch + SHA256 + atomic‑swap discipline** (factor it into `internal/updater`
   first so the binary updater and the content installer share one code path).
3. **Background auto‑update** (the "no manual step, like Claude Code"):
   - settings keys in `internal/settings`: `AutoUpdate *bool`, `UpdateChannel *string` (stable/latest),
     `LastUpdateCheck` timestamp.
   - `buttons update --install-agent` → writes `~/Library/LaunchAgents/sh.buttons.autoupdate.plist`
     (`StartInterval` + `RunAtLoad`) and `launchctl bootstrap`s it; macOS‑guarded.
   - `buttons update --background` → throttled, **silent on Homebrew/permission errors** (never nag the
     agent), logs to `~/.buttons/update.log`; the LaunchAgent runs **both** `buttons update` (binary)
     **and** `buttons store update --all` (content).
   - on‑run passive fallback: `cmd/root.go` `PersistentPreRunE` spawns a detached `--background` check
     when `LastUpdateCheck` is stale — fire‑and‑forget, never blocks the user.
4. **Resolution fixes (correctness):** (a) **flat‑namespace collision** → pack‑scope button names
   (`<pack>/<name>`) in `paths.go`/resolver; (b) **project‑vs‑global is winner‑take‑all and the docs are
   wrong about it** (code returns only the nearest `.buttons/`, never a union with `~/.buttons/`) → make
   it union/layered (or an explicit `--global`) so a globally‑installed autono pack isn't shadowed by any
   project `.buttons/`; (c) **resolution is name‑scoping only — it MUST NOT widen *data* scope:** a
   globally‑installed pack still binds each invocation to a **single principal's brain/lake partition**,
   resolved from the invoking identity, never shared across principals on a multi‑tenant machine. (The union
   resolver is also *step one* toward the architecture's `base→team→env→runtime→identity` **overlay** model —
   flat copy‑install satisfies the no‑symlink constraint but not yet overlays; §9.)
5. **MCP meta‑tools + `tags` + `buttons search`** (§3).

### Track B — make the autono buttons distributable (this repo)

The four decouplings from §1, in priority order:

1. **Ship `src/cli.ts` as an artifact.** `bun build --compile` it into a pinned `desk` binary (or
   publish `@autono/desk-cli`) that runs from any cwd against `~/.bobak-executive-desk`. Replace `bun
   src/cli.ts <x>` (54 buttons) with the pinned binary. **This is the single largest item and it gates
   which buttons ship in v1.**
2. **Secrets → batteries.** Rewrite `pd-proxy-request`'s `load_env` (and Plaid/Harvest/QBO peers) to read
   `BUTTONS_BAT_*` (keep the repo‑`.env` walk as a dev fallback). The pack's `requires_batteries[]` then
   drives an install‑time `buttons batteries set` prompt — requirements shipped, values entered once per
   machine.
3. **Kill cwd‑coupled peer resolution.** Drop the `cwd=…/autono-connectors` override in
   `cal-sync`/`meet-sync` (and ~15 `pd-*` dependents); press `pd-proxy-request` as a peer button declared
   via `requires` (works once Track A's resolver is cwd‑independent + pack‑scoped).
4. **Dedupe name collisions** before publishing (or rely on Track A's pack‑scoping).
5. **Prose move:** each pack ships a `PACK.md` authored from the former `SKILL.md` *body* (the
   connect/sync/query playbook) minus the skill‑discovery frontmatter; per‑button `AGENT.md` stays but
   is scrubbed of `bun src/cli.ts` / `.claude/skills` path references. The `CLAUDE.md` Buttons/ConceptML
   rules stay in **this** repo (they're project rules); cross‑agent discovery is the `agentskill.Install`
   markers, not copying `CLAUDE.md` into packs.

---

## 5. Packaging: how the ~124 buttons become packs

**N packs, one per former skill (~14), not one monolith** — because the store's unit is the button
folder and (once scoped) the pack is the cohesive install/version/uninstall unit:

- **`autono-connectors`** = the **base pack** (the `pd-*` toolkit); every other pack declares it as a
  `dependency`. This matches the real dependency seam.
- One pack per domain skill: `autono-cal`, `autono-finances`, `autono-meetings`, `autono-projects`,
  `autono-slack`, … installable independently (don't drag slides/whatsapp into finances).
- The 23 root buttons split: `brain-*`/`adb-*` → an **`autono-brain`** core pack (memory depends on it);
  `meetings-*` → fold into `autono-meetings`; dev tools (`typecheck`, `proxyman-curl`) → **`autono-dev`**.

Each pack = a **git‑tagged content repo** (private, trusted team to start) that `buttons store install`
pulls + SHA256‑verifies. Lowest‑effort publish path: point the store at the already‑committed `.buttons/`
trees and tag them — **no new artifact needed**. If you later want a release artifact, mirror the binary
pipeline (a `autono-buttons_<ver>.tar.gz` + checksums via the pack repo's own goreleaser), fetched the
same verified way.

---

## 6. OTA flow (end state)

```
launchd  sh.buttons.autoupdate.plist  (StartInterval 1h, RunAtLoad)
   └─ buttons update --background           # CLI binary  (existing self-updater)
   └─ buttons store update --all            # button packs (new, same fetch+sha256+atomic-swap)
        ↑ pulls the catalog + signed pack tarballs from the registry (one bearer key; #275)
on-run passive fallback: root PersistentPreRunE → detached --background check when LastUpdateCheck stale
```

Source is the **registry** (R2 + a thin Worker), **not** a git pull — one centrally‑rotatable bearer key, not
per‑machine GitHub tokens (`buttons-registry/docs/PLAN.md`). Channels are `stable`/`latest` pointers in the
catalog. Untrusted/public installs later add **signature verification** (`SECURITY.md` flags it as
not‑yet‑shipped).

**Architecture-required behaviors:**
- **Hot‑swap safety** — `store update` must not mutate a button mid‑press; in‑flight presses pin the version
  resolved at press start; a content update invalidates the *next* session's catalog, not the running one.
- **Yank reaches installed packs** — the updater honors a `yanked` signal and rolls a machine off a revoked
  `<name>@<version>` (fail‑closed), not just blocking new installs.
- **Structured audit, not a free‑text log** — install/update/press/publish each emit an event into the minimal
  observability envelope `{ event_id, occurred_at, actor, principal, runtime, component,
  config_version=pack@version, content_hash, decision/result-class }` (`pack.installed`, `pack.updated`,
  `runtime.synced`, `button.pressed`, `pack.published`). **IDs + versions + result‑class only — never battery
  values or raw sensitive stdout.** So the toolset version a runtime ran survives its teardown (Ex.18).

---

## 7. How this fits the drafted architecture (and where it deliberately stops)

Buttons‑as‑distribution is the **first concrete slice of the control plane — but only the config /
materialization / mount‑time slice.** The architecture's load‑bearing boundary is **config vs. execution**:
the control plane *describes and materializes* what a runtime may mount; it **never executes**. So the
mapping must be **split, not collapsed onto "Buttons"**:

| Architecture | Buttons realization | Plane |
|---|---|---|
| Control plane / config + tool registry (mount‑time) | `buttons store` · `pack.json` · the registry — distribute + version *which* button surfaces a runtime may mount | **config — no execution** |
| Runtime / execution (call‑time) | `buttons press` · the `buttons_press` MCP tool — actually *runs* a button | **runtime — NOT the control plane** |
| Secret bindings | `batteries` materialize the *injection* half; the pack manifest carries the full binding contract (`provider_ref`, `inject_as`, `environments`, `allowed_runtimes`, `rotation_hint`) — **requirements/refs, never values** | config |
| Runtime adapter | Buttons‑on‑local is **one** adapter (generic‑local); the pack format stays runtime‑neutral so OpenClaw/Codex/Hermes/Docker adapters render the same packs into native config (e.g. bindings → OpenClaw SecretRefs). The Buttons MCP server is one **mount**, not THE adapter. | config→native |
| Observability spine | installed `pack@version` + `content_hash` **are** the `config_version`/`toolset_version`; every press + OTA + publish stamps them into a structured audit event | telemetry |

**Mount‑time vs. call‑time — the split the store does NOT cross.** `buttons store install` is *mount‑time*:
it materializes *which* buttons a runtime *may* mount. Whether a specific actor/session may *press* a button
that touches a shared, sensitive, or data‑service resource is a **call‑time** decision the architecture
assigns downstream (IAM / a data service / a tool gateway / an authz‑aware MCP). Today each button's own
confirm‑gate is the only call‑time control; the target is a shared authorizer. **Local, self‑contained,
provider‑native buttons** (the plan's sweet spot) are the fast path with no platform IAM in the hot loop;
**governed buttons** must obtain a call‑time decision before executing.

**Possession ≠ authorization.** Installing a pack and holding its batteries is credential *possession* — not
the right to exercise a capability. The store deliberately does **not** collapse the two (the exact shape the
IAM notes reject). A governed button maps its press to a *capability + resource + action* and asks a shared
authorizer for a decision — it must **not** invent per‑integration grant logic (coarse‑mount / fine‑enforce).

**Buttons is not a data plane.** Brain/lake‑touching buttons (sync/graph/`adb‑*`) are data‑service *consumers*:
once the data service exists they route through it (the button *resolves + invokes*; the service *governs +
executes*, injecting per‑principal partition/scope). `pack.json` declares only the **coarse adapter surface** —
capability families, secret *names*, token audience — and **never** the data‑service resource catalog (tables,
fields, AutonoDB scopes) or row/field policy.

**Resolved runtime contract — partial.** `store.lock` (installed + pinned packs) is a *partial* resolved
contract: it pins the tool/capability axis but does **not** yet carry prompt/model refs, persona, IAM context,
or token audiences — which the architecture requires for a fully versioned, globally‑rollback‑able agent
snapshot. Those live in `CLAUDE.md`/`agentskill` markers today (a gap — §9).

**Honesty clause — what the store deliberately leaves to IAM / data‑service / tool‑gateway:** call‑time
authorization, actor‑vs‑principal carriage, token brokering, per‑resource redaction/limits/grants. Naming these
as out‑of‑scope (not silently absent) is what keeps Buttons‑as‑distribution a faithful *slice* of the control
plane rather than an overclaim that it *is* the control plane.

### How packs explain the canonical examples (the design test suite)
- **Ex.6 local personal coding agent** — self‑contained packs mount **local‑provider‑native** tools, no
  platform IAM in the hot path. *The plan's sweet spot.*
- **Ex.1 Slack CoS free/busy** — installing an `autono-cal` pack only **mounts** the surface; the per‑call
  *busy‑only* redaction + grant is **not** something the store or the button provides — it exposes the
  call‑time gap a downstream authorizer must close.
- **Ex.18 ephemeral remote agent** — pack pins + secret bindings give boot‑time materialization, but
  **accountability/audit must survive teardown**: install/update/press emit durable central events
  (machine, `pack@version`, `content_hash`), not just a local log.

---

## 8. Phased roadmap — store-first

> **Sequencing decision (2026‑06‑22): build the store in `autonoco/buttons` FIRST and validate it with a
> trivial pack, THEN migrate our buttons onto it.** Rationale: the store is the foundation (no
> versioning/install/OTA without it); a manual‑copy "proof" only re‑proves the `desk` binary and never
> exercises the real path; and we must not migrate 124 buttons against a contract (pack format,
> name‑scoping, dep resolution) that doesn't exist yet — define + validate it once. The `desk` binary
> (the only content prereq independent of the store) is already done.

| # | Repo | Deliverable | Gate it clears |
|---|---|---|---|
| **1. Schema** | `buttons` | `Version`/`Tags`/`Requires`/`RequiresBatteries` on `button.json` + a new `pack.json` | the versioned/pinnable unit exists |
| **2. Store** | `buttons` | `buttons store add/install/update` (`#274`, reuse `update.go`'s fetch+SHA256+atomic‑swap) + resolver fixes (pack‑scoped names, global∪project union) | one‑command install works |
| **3. Validate** | both | a **trivial throwaway pack** installs + updates + auto‑updates end‑to‑end | go/no‑go: "buttons + store actually solve it" *before* mass migration |
| **4. Migrate** | `desk` | our buttons → packs: `desk` ✓ · secrets→batteries · drop cwd hacks · dedupe names · publish | the autono packs ship via the store, done **once** against the real contract |
| **5. OTA** | `buttons` | settings + `--install-agent` launchd + `--background` + on‑run passive + private‑repo asset fix | hands‑off auto‑update |
| **6. Discovery** | `buttons` | MCP meta‑tools (CLI/MCP parity) + `tags` + `buttons search` | fixes the trigger‑reliability pain at the root |

Steps 1–3, 5–6 are the `#274` store work in the Buttons repo; step 4 is this repo's content migration,
done once against the validated store.

---

## 9. Decisions before building

1. **`bun build --compile` desk binary vs `@autono/desk-cli` npm** — recommend the compiled binary
   (no node/bun dependency on target machines; pin by version; mirrors how `buttons` itself ships).
2. **Define the store spec** — the code (`#274`, Phase 5.1) and `SECURITY.md` (Phase 3) disagree and
   nothing is specified. You get to define it; recommend git‑ref pull + `pack.json` + SHA256 now,
   signatures when you add untrusted installs.
3. **v1 pack scope** — start with `autono-connectors` (base) + the brain‑query/`pd-*` self‑contained
   buttons; defer the TS‑CLI sync buttons to Phase 0's completion.
4. **Pack‑scoping vs author‑time prefixes** for the name collisions — recommend the engine pack‑scope
   (the right fix) over renaming buttons.
5. **Build order** — Track A and Track B are independent and parallelizable; the store (A2) and the desk
   CLI (B1) are the two critical‑path items.

Architecture-alignment deferrals (named, not silently absent):

6. **Call‑time authorizer for governed buttons** — a pre‑exec `authorize(actor, principal, capability,
   resource, action)` hook governed buttons call before `main.sh`; local provider‑native buttons are exempt.
7. **Brokered / service‑side secret bindings** — batteries inject env vars only; a credential the runtime
   must *never receive* (brokered exchange, service‑side adapter) isn't yet expressible.
8. **Prompt/model/persona axis** — the resolved contract should pin these too; today they live in
   `CLAUDE.md`/`agentskill`. Should they become a pack (or a sibling agent‑definition artifact)?
9. **Registry token model** — static shared read key (MVP) → short‑lived audience‑bound minted tokens per
   machine before any non‑read scope / untrusted fleet.
10. **Hot‑swap + session/catalog invalidation** — `store update` must not mutate a button mid‑press (in‑flight
    presses pin the version at start); a content update invalidates the *next* session's catalog, not the
    running one.
11. **Yank / revocation reaches installed packs** — a `yanked` signal the background updater honors
    (fail‑closed), not just blocking new installs.
12. **Non‑local runtime adapters** — OpenClaw/Codex/Hermes/Docker render the same packs into native config
    (tool policy, SecretRefs, sandbox profiles); out of v1, but the pack format must not bake in
    Buttons/flat‑files assumptions.
