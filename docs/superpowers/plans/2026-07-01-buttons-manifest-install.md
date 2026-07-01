# Manifest-Based Buttons Install Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Buttons install work like a package manager: `buttons add` writes `.buttons/buttons.json`, `buttons install` reads `.buttons/buttons.json`, and installed `button.json` files remain the runtime specs inside each button folder.

**Architecture:** `.buttons/buttons.json` is the desired dependency manifest. `.buttons/buttons-lock.json` is the exact resolved set. `.buttons/buttons/<name>/button.json` is the installed button spec and should not be the source of truth for registry resolution. Registry selection should not be a normal user-facing flag; it should come from workspace/bootstrap config, with manifest dependencies carrying package identity.

**Tech Stack:** Go CLI, JSON manifest/lock files, existing `internal/store` HTTP registry source, existing updater package, Mintlify docs.

---

## Product Decision

The current draft PR `#222` implemented a per-button-only install model. That model is now wrong for the desired product shape.

Correct model:

```bash
buttons add @autono/hello
buttons add @autono/deploy@1.2.3
buttons install
```

- `buttons add @autono/hello` updates `.buttons/buttons.json` and installs immediately, like `bun add`.
- `buttons add @autono/hello@1.2.3` pins that package to an exact version.
- `buttons install` has no package argument. It reads `.buttons/buttons.json` and materializes everything in it, using `.buttons/buttons-lock.json` for repeatability.
- `buttons update` updates floating dependencies only. Exact versions are pins and do not move unless `buttons add @scope/name@new-version` changes the manifest.
- `--source` and `$BUTTONS_SOURCE` should be removed from the public user flow. Local/dev installs should use a test registry or internal-only helpers, not user-facing flags.

## Schemas

### `.buttons/buttons.json`

This is the root project manifest. It is committed.

Minimal MVP schema:

```json
{
  "schema_version": 1,
  "dependencies": {
    "@autono/hello": "latest",
    "@autono/deploy": "1.2.3"
  }
}
```

Field contract:

| Field | Type | Required | Meaning |
|---|---:|---:|---|
| `schema_version` | integer | yes | Manifest format version. Start at `1`. |
| `dependencies` | object | yes | Map of scoped package name to desired version policy. Keys are `@desk/name`. |

Dependency value contract:

| Value | Meaning |
|---|---|
| `"latest"` | Floating dependency. `buttons install`, `buttons update`, passive OTA, and push-triggered OTA may move it to the latest published version. |
| `"1.2.3"` | Exact pin. Install resolves only that immutable version; update/status may report newer versions as informational but must not mutate it. |

Deferred, not MVP:

- Semver ranges like `"^1.2.0"`.
- Per-dependency object options like `{ "version": "...", "auto_update": true }`.
- Multiple registries in the manifest.

Reason: `latest` versus exact version is enough to express "auto-update" versus "pinned" with the fewest variables.

### `.buttons/buttons-lock.json`

This is the exact resolved install set. It is committed. It is not user-authored, but it is part of the package-manager model and required for repeatable installs.

```json
{
  "schema_version": 1,
  "dependencies": {
    "@autono/hello": {
      "kind": "button",
      "requested": "latest",
      "version": "1.2.3",
      "content_hash": "sha256:4f8c...",
      "installed_name": "hello",
      "resolved_at": "2026-07-01T12:00:00Z"
    },
    "@autono/deploy": {
      "kind": "button",
      "requested": "1.2.3",
      "version": "1.2.3",
      "content_hash": "sha256:91ab...",
      "installed_name": "deploy",
      "resolved_at": "2026-07-01T12:00:00Z"
    }
  }
}
```

Field contract:

| Field | Type | Required | Meaning |
|---|---:|---:|---|
| `schema_version` | integer | yes | Lock format version. Start at `1`. |
| `dependencies` | object | yes | Exact resolved dependency graph keyed by `@desk/name`. |
| `kind` | string | yes | Registry unit kind: `button` or `drawer`. Comes from the registry index. |
| `requested` | string | yes | The manifest value that produced the resolution, e.g. `latest` or `1.2.3`. |
| `version` | string | yes | Exact immutable version installed. |
| `content_hash` | string | yes | Hash verified before install and used for drift checks. |
| `installed_name` | string | yes | Local folder/display name, usually unscoped `name`. |
| `resolved_at` | string | yes | RFC3339 timestamp for observability. |

Do not store registry bearer keys or battery values in the lock file.

### `.buttons/buttons/<name>/button.json`

This is the per-button runtime/package spec. It lives inside each installed or locally authored button. It is not the project manifest.

MVP package/runtime schema:

```json
{
  "schema_version": 1,
  "name": "hello",
  "version": "1.2.3",
  "description": "Say hello",
  "runtime": "shell",
  "args": [
    { "name": "name", "type": "string", "required": true }
  ],
  "env": {},
  "timeout_seconds": 60,
  "requires": {
    "@autono/base": "latest"
  },
  "requires_batteries": [
    "OPENAI_API_KEY"
  ]
}
```

Field contract:

| Field | Type | Required | Meaning |
|---|---:|---:|---|
| `schema_version` | integer | yes | On-disk button spec version. Start at `1`. |
| `name` | string | yes | Local button name, usually bare/unscoped. |
| `version` | string | required for publish/install | Button package version. Exact immutable artifact version once published. |
| `description` | string | no | Human-readable description. |
| `runtime` | string | yes for runnable buttons | `shell`, `python`, `node`, `prompt`, or empty for HTTP buttons identified by `url`. |
| `args` | array | no | Press-time arguments. |
| `env` | object | no | Non-secret environment defaults. |
| `timeout_seconds` | integer | no | Press timeout. |
| `requires` | object | no | Transitive package dependencies, keyed by `@desk/name`, valued as `latest` or exact version. Replaces the current array shape. |
| `requires_batteries` | array | no | Secret key names required at press time. Values never ship. |
| `url`, `method`, `headers`, `body`, `allowed_host`, `max_response_bytes`, `allow_private_networks` | mixed | no | HTTP button fields. |
| `mcp_enabled` | boolean | no | Expose through `buttons mcp`. |
| `queue` | object | no | Concurrency control. |
| `output_schema` | object | no | JSON Schema for stdout-as-JSON. |
| `triggers` | array | no | Automatic button triggers under `buttons serve`. |
| `created_at`, `updated_at` | string | no | Local authoring timestamps. |

Fields to remove from the new model:

| Field | Reason |
|---|---|
| `source` | Registry/source belongs in manifest/lock/bootstrap config, not every installed button. |
| `source_name` | Lock file maps scoped package identity to local installed name. |
| `content_hash` | Lock file owns content hash and repeatable install state. |

Existing code may have these fields during the transition, but the desired model should not rely on them.

## Command Surface

Keep the user-facing surface small:

```text
buttons add @desk/name
buttons add @desk/name@1.2.3
buttons install
buttons remove @desk/name
buttons status
buttons update
buttons publish @desk/name
```

Do not expose:

```text
buttons install @desk/name
buttons install --source <dir>
buttons publish --source <dir>
BUTTONS_SOURCE
```

`buttons install @desk/name` should fail with:

```text
Use `buttons add @desk/name` to add a dependency, or `buttons install` to install from .buttons/buttons.json.
```

## Checklist

### Task 1: Freeze the Wrong PR Shape

**Files:**
- Modify PR state only: `autonoco/buttons#222`

- [x] Confirm PR `#222` is draft.
- [x] Add a PR comment saying the implementation model is being reworked from per-button stamps to manifest/lock install.
- [x] Do not merge `#222` until this plan is implemented.

### Task 2: Add Manifest and Lock Packages

**Files:**
- Create: `internal/manifest/manifest.go`
- Create: `internal/manifest/manifest_test.go`
- Create: `internal/manifest/lock.go`
- Create: `internal/manifest/lock_test.go`

- [x] Implement `Manifest` with `SchemaVersion int` and `Dependencies map[string]string`.
- [x] Validate dependency keys as scoped names: `@desk/name`.
- [x] Validate dependency values as `latest` or exact semver.
- [x] Implement atomic read/write for `.buttons/buttons.json`.
- [x] Implement `Lockfile` with exact resolved dependency entries.
- [x] Implement atomic read/write for `.buttons/buttons-lock.json`.
- [x] Test missing manifest, malformed JSON, invalid dependency key, invalid dependency value, stable pretty-print, and atomic write.

### Task 3: Add `buttons add`

**Files:**
- Create: `cmd/add.go`
- Create/modify: `cmd/add_test.go`
- Modify: `cmd/root.go`
- Modify: `docs/cli/buttons_add.md`
- Modify: `docs/docs.json`

- [x] Parse `buttons add @desk/name` as dependency `@desk/name = latest`.
- [x] Parse `buttons add @desk/name@1.2.3` as dependency `@desk/name = 1.2.3`.
- [x] Reject unscoped names for MVP unless a default desk has been explicitly configured later.
- [x] Create `.buttons/buttons.json` if missing.
- [x] Update the dependency entry.
- [x] Immediately run the same reconciliation path as `buttons install`.
- [x] Append history event `add`.
- [x] Print a concise success message.

### Task 4: Rework `buttons install`

**Files:**
- Modify: `cmd/install.go`
- Modify: `cmd/install_test.go`
- Modify: `internal/store/install.go`
- Modify/add tests under `internal/store`
- Modify: `docs/cli/buttons_install.md`

- [x] Change `buttons install` to accept no package argument.
- [x] Make `buttons install @desk/name` fail with the `buttons add` guidance.
- [x] Remove public `--source`.
- [x] Remove `$BUTTONS_SOURCE` from the command behavior and docs.
- [x] Read `.buttons/buttons.json`.
- [x] Resolve each manifest dependency against the configured registry.
- [x] Respect `buttons-lock.json` for exact pinned installs when the manifest and lock agree.
- [x] Verify content hash before writing installed files.
- [x] Write/update `.buttons/buttons-lock.json`.
- [x] Install transitive `button.json.requires` dependencies.
- [x] Append history event `install`.

### Task 5: Move Update Logic to Manifest/Lock

**Files:**
- Modify: `internal/updater/content.go`
- Modify: `internal/updater/content_test.go`
- Modify: `cmd/status.go`
- Modify: `cmd/update.go`
- Modify: `cmd/passive_update.go`

- [x] Make `buttons status` read manifest + lock, not installed `button.json.source`.
- [x] For `"latest"` deps, report update available when registry latest is newer than lock.
- [x] For exact pinned deps, report newer versions as informational, not updateable.
- [x] Make `buttons update` update only floating deps.
- [x] Keep CLI binary self-update behavior.
- [x] Make passive OTA call the same manifest/lock update path.
- [x] Make push-triggered OTA wake receivers call the same manifest/lock update path.
- [x] Preserve local modification safety by comparing installed content to lock hash before overwriting.

### Task 6: Simplify `button.json` Install Metadata

**Files:**
- Modify: `internal/button/entity.go`
- Modify: `internal/store/install.go`
- Modify tests that assert `source`, `source_name`, or `content_hash`.
- Modify: `docs/concepts/button-json.mdx`

- [x] Remove `source`, `source_name`, and `content_hash` from the desired installed `button.json` schema.
- [x] Keep `version` as the package version.
- [x] Change `requires` from `[]string` to `map[string]string` for package-manager semantics.
- [x] Update publish validation to require `version`.
- [x] Update transitive dependency resolution to read `requires` as package refs with version policy.

### Task 7: Registry Resolution

**Files:**
- Modify: `internal/store/http_source.go`
- Modify: `internal/store/source.go`
- Modify tests under `internal/store`
- Coordinate with: `/Users/bobakemamian/Downloads/projects/autono-github/buttons-registry`

- [x] Ensure `GET /v1/index` carries `name`, `kind`, `version`, and `sha256`.
- [x] Resolve `latest` by choosing the registry's latest entry for a name.
- [x] Resolve exact versions directly.
- [x] Return `kind` so install can choose button vs drawer behavior.
- [x] Keep registry bearer auth in batteries/settings, not in `buttons.json`.

### Task 8: Docs Rewrite

**Files:**
- Modify: `README.md`
- Modify: `docs/concepts/registry.mdx`
- Modify: `docs/concepts/folder-structure.mdx`
- Modify: `docs/concepts/button-json.mdx`
- Modify: `docs/concepts/history.mdx`
- Modify: `docs/cli/buttons_add.md`
- Modify: `docs/cli/buttons_install.md`
- Modify: `docs/cli/buttons_update.md`
- Modify: `docs/cli/buttons_status.md`
- Modify registry docs in `/Users/bobakemamian/Downloads/projects/autono-github/buttons-registry/docs/PLAN.md`
- Modify registry docs in `/Users/bobakemamian/Downloads/projects/autono-github/buttons-registry/docs/ota-auto-update-and-publish.md`

- [x] Replace every "no buttons.json" statement with the manifest model.
- [x] Remove public `--source` docs.
- [x] Show `buttons add` as the way to add a button.
- [x] Show `buttons install` as the way to sync from manifest.
- [x] Document `latest` versus exact version.
- [x] Document `buttons-lock.json`.
- [x] Document `button.json` as the inner per-button spec, not the project manifest.
- [x] Keep public docs on `registry.example` or product-level placeholders unless the real production URL is ready to expose.

### Task 9: End-to-End Smoke

**Files:**
- Modify/create: `test/integration/manifest_install_test.go`
- Modify/create: `scripts/smoke-manifest-ota.sh`

- [x] Start a local HTTP registry test server.
- [x] Publish/install `@autono/hello@1.0.0`.
- [x] Run `buttons add @autono/hello`.
- [x] Assert `.buttons/buttons.json` contains `"@autono/hello": "latest"`.
- [x] Assert `.buttons/buttons-lock.json` resolves `1.0.0`.
- [x] Assert `.buttons/buttons/hello/button.json` exists.
- [x] Publish `@autono/hello@1.1.0`.
- [x] Run `buttons status` and assert it reports an available update.
- [x] Run `buttons update`.
- [x] Assert lock resolves `1.1.0`.
- [x] Assert the installed button content changed.
- [x] Repeat with `buttons add @autono/hello@1.0.0` and assert update does not move it.

## Acceptance Criteria

- [x] `buttons add @autono/<name>` updates `.buttons/buttons.json` and installs immediately.
- [x] `buttons add @autono/<name>@1.2.3` writes an exact pin.
- [x] `buttons install` with no args reads `.buttons/buttons.json` and materializes all dependencies.
- [x] `buttons install @autono/<name>` errors with guidance to use `buttons add`.
- [x] `buttons.json` is the desired dependency manifest.
- [x] `buttons-lock.json` records exact resolved versions and hashes.
- [x] Installed `button.json` is the per-button runtime/package spec, not the dependency manifest.
- [x] `button.json` no longer needs `source`, `source_name`, or `content_hash` for update resolution.
- [x] `buttons status` reports updateability from manifest + lock.
- [x] `buttons update` mutates only floating `"latest"` dependencies.
- [x] Exact version pins do not auto-update.
- [x] Passive OTA and push-triggered OTA run the same manifest/lock update path.
- [x] Public docs no longer teach `--source` or `$BUTTONS_SOURCE`.
- [x] `go test ./...` passes in `buttons`.
- [x] Registry tests pass in `buttons-registry`.
- [x] Live smoke proves `latest` moves from `1.0.0` to `1.1.0`, while exact `1.0.0` stays pinned.
