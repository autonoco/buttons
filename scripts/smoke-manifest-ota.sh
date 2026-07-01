#!/usr/bin/env sh
set -eu

cd "$(dirname "$0")/.."

go test ./test/integration -run 'TestUpdate(RefreshesFloatingDependency|DoesNotMoveExactPin)$' -count=1
