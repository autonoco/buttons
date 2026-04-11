BINARY_NAME := buttons

# Build metadata injected into cmd/version.go via -ldflags -X.
#
# VERSION: prefers a git tag at HEAD, falls back to a short SHA (with
#          -dirty suffix if the working tree is dirty), falls back to
#          "dev" if git is unavailable (e.g., `go install` from a pure
#          module download).
# COMMIT:  full git SHA at HEAD, or "unknown" if git is unavailable.
# DATE:    reproducible UTC build timestamp in ISO 8601. Accepts a
#          pre-set SOURCE_DATE_EPOCH for reproducible-build workflows.
#
# `?=` lets CI/goreleaser override any of these via environment vars.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || echo unknown)

# -s strips the symbol table; -w strips the DWARF debug info. Together
# they shave roughly 25-30% off the release binary size. Rebuild without
# them (`go build -o buttons .`) if you need symbols for debugging.
LDFLAGS := -s -w \
	-X github.com/autonoco/buttons/cmd.version=$(VERSION) \
	-X github.com/autonoco/buttons/cmd.commit=$(COMMIT) \
	-X github.com/autonoco/buttons/cmd.date=$(DATE)

.PHONY: build run clean test integration test-all

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY_NAME) .

run: build
	./$(BINARY_NAME)

clean:
	rm -f $(BINARY_NAME)

test:
	go test ./internal/...

integration: build
	go test -v -count=1 ./test/integration/

test-all: test integration
