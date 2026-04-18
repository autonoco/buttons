# UI update plan — board → full spec

> Staged plan to bring `internal/tui/` up to the [IMPLEMENTATION.md](./IMPLEMENTATION.md) spec.
> Six groups, eight PRs, ordered by dependency + user value.

## Current state vs spec

**Shipped (✅):** board layout, pinned + list, empty-state hero, `L` logs pane
(5-row peek), spinner, auto-refresh, ambient no-quit footer, session ✓/✗ glyphs,
click handling for pinned cards / list rows / footer press pill, batteries
injection at press-time.

**Spec but missing (❌):**

1. Mechanical press choreography (4-frame pulse via `tea.Tick`)
2. Elapsed timer on active cards (`● active · 3.2s`)
3. Press-with-args inline form — today we bail to the CLI
4. Detail view as a TUI page — today we print to stderr from `cmd/detail.go`
5. Full-screen `buttons logs <name>` with follow / top / end keys
6. `engine.Execute` line-channel streaming (pre-req for #5)
7. Themes via `BUTTONS_THEME` (paper / phosphor / amber / default)
8. `StatusWarn` style and warn severity in the log stream
9. Snapshot + `teatest` integration tests

---

## Groups

### Group A · Board polish (3 small PRs, parallelizable)

| PR  | Scope                                                                                                                                                                                                         | Files |
|-----|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|---|
| A1  | **Mechanical press choreography.** `pressPulse` model field + `pressFireMsg` + `pressReleaseMsg`, 40 / 80 / 180 ms `tea.Tick` schedule. Reuses existing styles (Normal → Thick → Indicator → Active). No new colors. | `internal/tui/board.go`, `internal/tui/styles.go` |
| A2  | **Elapsed timer on active cards.** Track `pressStartedAt map[name]time.Time`. On running state, render `· <elapsed>s` on pinned card + list row. Driven by the existing spinner tick — no new ticker.          | `internal/tui/board.go` |
| A3  | **`StatusWarn` style.** Add warn color (`#F0C060`) to `Styles`. No consumer yet — reserved for the log-severity plumbing in Group C. Pure style addition.                                                     | `internal/tui/styles.go` |

### Group B · Themes (depends on A3)

| PR  | Scope | Files |
|-----|---|---|
| B1  | **`BUTTONS_THEME` env var (paper / phosphor / amber / default).** Extend `BuildStyles` with `themeFromEnv()`, branch on palette only — style graph stays identical. Amber / phosphor shift the indicator warm so orange doesn't vibrate on green. | `internal/tui/styles.go` |

### Group C · Live logs (biggest slice — do after A + B)

| PR  | Scope | Files |
|-----|---|---|
| C1  | **Engine line-channel streaming.** `engine.Execute` gains a `LineSink chan<- LogLine` parameter. Shell / HTTP / prompt executors write line-by-line alongside the `Stdout` / `Stderr` buffer. No JSON contract change. Unit test asserts interleaved delivery with severity tags. | `internal/engine/execute.go`, new `internal/engine/stream.go`, all callers (pass `nil` to keep behavior) |
| C2  | **`buttons logs <name>` TUI + `internal/tui/logs.go`.** New `LogsModel`. Subscribes to line channel via `streamLogs` `tea.Cmd`. Keys: `f` follow, `g` / `G` top/end, `esc` / `q` back, `ctrl+c` cancel press. Header (button · press id · elapsed) + stream + footer (cancel pill + hint line). Wire an invocation path from board `↵` on an active row. | new `internal/tui/logs.go`, new `cmd/logs.go` |

### Group D · Inline press form

| PR  | Scope | Files |
|-----|---|---|
| D1  | **Press-with-args form.** Replace the `requires --arg X; press from CLI` bail with a `huh`-powered inline form: one field per required arg, Tab cycles, ↵ runs, ⎋ cancels. `will run:` preview line shows the CLI equivalent. On submit, same code path as the CLI press. | new `internal/tui/argform.go`, `internal/tui/board.go` |

### Group E · Detail-as-TUI-page

| PR  | Scope | Files |
|-----|---|---|
| E1  | **TUI detail page reached via `e` from board / `buttons <name>`.** Replace ad-hoc stderr printing in `cmd/detail.go` with a Bubble Tea page: runtime / method / URL / timeout / max-resp / args / last run. `↵ press`, `e edit`, `h history`, `⎋ back` keybinds. CLI `--json` path untouched. | new `internal/tui/detail.go`, `cmd/detail.go` |

### Group F · Testing foundation (parallel with anything)

| PR  | Scope | Files |
|-----|---|---|
| F1  | **Snapshot + `teatest` harness.** Add `charm.land/x/ansi` for ANSI stripping, a snapshot helper, and a `teatest` helper. Seed snapshots for: idle board, populated board with cursor, empty hero, logs pane open, active press, fail state. Every new render going forward gets a snapshot. | new `internal/tui/snapshot_test.go`, `internal/tui/testdata/*.golden` |

---

## Dependencies + merge order

```
A1 ─┐
A2 ─┼── merge in any order
A3 ─┘
       │
       ▼
       B1            depends on A3 (warn color contract)
       │
       ▼
       C1            pre-req for C2 (engine surface area)
       │
       ▼
       C2
       │
       ▼
D1, E1, F1           parallel, each ships on its own
```

---

## Rough scope

| Group | PRs | LOC  | Risk                                                  |
|-------|----:|-----:|-------------------------------------------------------|
| A     | 3   | ~250 | Low — model fields + render branches                  |
| B     | 1   | ~120 | Low — color table                                     |
| C     | 2   | ~600 | **High** — engine surface change + new TUI program    |
| D     | 1   | ~200 | Medium — form UX, validation UX                       |
| E     | 1   | ~180 | Low-medium — detail is already modeled                |
| F     | 1   | ~150 | Low — golden files                                    |

---

## Gaps vs spec to settle before starting each group

1. **Engine streaming contract (C1)** — spec says "line channel" but not:
   severity classification (info / stdout / stderr / warn — how is warn
   detected? keyword match? explicit tag?), timestamp source (child stdout
   receive time vs. parent monotonic clock), back-pressure (drop if TUI can't
   drain vs. block). Short design note lands with C1.
2. **Detail view `e edit` (E1)** — does "edit" mean open `$EDITOR` on the code
   path, or launch `buttons update`? Default: `$EDITOR` on the code file —
   `update` is flag-driven and wrong for a TUI.
3. **Theme discoverability (B1)** — `BUTTONS_THEME` env works for power users
   but is invisible to new ones. Extend the settings store added in #69:
   `buttons config set theme paper`. Env var wins when set, settings next,
   default-detected third.

---

## Anti-goals (stay out of these)

- Progress bars in the board — per spec, we don't know the percent.
- A tab bar on logs view — `grep` exists; we're a tail, not a dashboard.
- A quit key in the footer legend — `ctrl+c` stays unadvertised.
- Per-frame animation engines — four `tea.Tick` scheduled swaps are plenty.
- Hardware bezel in the TUI — marketing chrome only. Stop at the screen.
