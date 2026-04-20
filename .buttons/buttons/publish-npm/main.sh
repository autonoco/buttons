#!/bin/sh
# Publish Buttons to npm — wraps scripts/publish-npm.mjs so the press
# surface is "a button with typed args" instead of "remember the env
# var name and invocation path."
#
# Invariants (same as the underlying script):
#   - goreleaser has run and dist/ + dist/artifacts.json exist
#   - `node` and `npm` are on PATH
#   - CI authenticates via npm Trusted Publishing (OIDC) — no NPM_TOKEN
#
# Args:
#   VERSION  bare semver, no leading "v" (e.g. "0.11.0")
#   DRY_RUN  optional; "true"/"1" adds --dry-run
set -eu

# cd to the repo root regardless of where the press was invoked from.
# The node script uses path arithmetic off its own __dirname so as long
# as scripts/publish-npm.mjs isn't moved, this works from any subdir.
cd "$(git rev-parse --show-toplevel)"

# Forward the optional dry-run flag. Any truthy value from the CLI is
# normalised to --dry-run; anything else skips the flag entirely.
flags=""
case "${BUTTONS_ARG_DRY_RUN:-}" in
	1|true|yes|TRUE|YES) flags="--dry-run" ;;
esac

# VERSION is the contract with the node script — it reads process.env.VERSION.
VERSION="$BUTTONS_ARG_VERSION" node scripts/publish-npm.mjs $flags
