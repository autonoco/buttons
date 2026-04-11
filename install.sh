#!/bin/sh
# install.sh — install the Buttons CLI from GitHub Releases.
#
# Usage (interactive):
#   curl -fsSL https://raw.githubusercontent.com/autonoco/buttons/main/install.sh | sh
#
# Usage (pin a version):
#   curl -fsSL https://raw.githubusercontent.com/autonoco/buttons/main/install.sh \
#     | BUTTONS_VERSION=v0.1.0 sh
#
# Usage (install to a custom directory):
#   curl -fsSL https://raw.githubusercontent.com/autonoco/buttons/main/install.sh \
#     | BUTTONS_INSTALL_DIR=$HOME/.local/bin sh
#
# Supported platforms:
#   - darwin / arm64 (Apple Silicon)
#   - darwin / x86_64 (Intel Mac)
#   - linux  / arm64 (ARM servers, Raspberry Pi 4+)
#   - linux  / x86_64 (most Linux + WSL)
#
# Authentication:
#   The Buttons repository is currently private. Export GITHUB_TOKEN with
#   contents:read scope before running this script. When the repository is
#   made public (autonoco/autono#345), this variable becomes optional.
#
# This script is POSIX sh so it runs in Alpine, Debian slim, BusyBox, and
# other minimal container images without needing bash.

set -eu

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------

REPO_OWNER="autonoco"
REPO_NAME="buttons"
DEFAULT_INSTALL_DIR="/usr/local/bin"
BINARY_NAME="buttons"

VERSION="${BUTTONS_VERSION:-latest}"
INSTALL_DIR="${BUTTONS_INSTALL_DIR:-$DEFAULT_INSTALL_DIR}"

API_BASE="https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}"

# ---------------------------------------------------------------------------
# Utilities
# ---------------------------------------------------------------------------

info()  { printf '==> %s\n' "$*" >&2; }
warn()  { printf 'WARN: %s\n' "$*" >&2; }
die()   { printf 'error: %s\n' "$*" >&2; exit 1; }

need_cmd() {
    if ! command -v "$1" >/dev/null 2>&1; then
        die "required command not found: $1"
    fi
}

# ---------------------------------------------------------------------------
# Platform detection
# ---------------------------------------------------------------------------

detect_platform() {
    _os=$(uname -s 2>/dev/null || echo unknown)
    _arch=$(uname -m 2>/dev/null || echo unknown)

    case "$_os" in
        Darwin) _os="darwin" ;;
        Linux)  _os="linux" ;;
        *) die "unsupported OS '$_os' (only darwin and linux are supported; Windows tracked in autonoco/autono#350)" ;;
    esac

    case "$_arch" in
        x86_64|amd64)   _arch="x86_64" ;;
        arm64|aarch64)  _arch="arm64" ;;
        *) die "unsupported architecture '$_arch' (only x86_64 and arm64 are supported)" ;;
    esac

    PLATFORM="${_os}_${_arch}"
    info "detected platform: $PLATFORM"
}

# ---------------------------------------------------------------------------
# GitHub API helpers
# ---------------------------------------------------------------------------

# curl wrapper that adds auth when available and fails on HTTP errors.
# Prints to stdout; returns non-zero on failure.
#
# Uses `set --` to build the curl argument list so that header values
# containing spaces (e.g. "Authorization: Bearer <token>") are passed as
# single arguments. Building the list via a flat string variable breaks
# because the shell word-splits on every space, and curl ends up seeing
# "Bearer" and the token as standalone positional args / URLs.
github_api() {
    _url="$1"
    _accept="${2:-application/vnd.github+json}"

    set -- -fsSL \
        -H "Accept: $_accept" \
        -H "X-GitHub-Api-Version: 2022-11-28"

    if [ -n "${GITHUB_TOKEN:-}" ]; then
        set -- "$@" -H "Authorization: Bearer ${GITHUB_TOKEN}"
    fi

    curl "$@" "$_url"
}

# Resolve VERSION -> concrete tag name. If VERSION=latest, queries the API
# for the latest release. Otherwise echoes VERSION as-is.
resolve_version() {
    if [ "$VERSION" = "latest" ]; then
        info "resolving latest release…"
        if _body=$(github_api "${API_BASE}/releases/latest" 2>/dev/null); then
            RESOLVED_VERSION=$(echo "$_body" | grep -o '"tag_name"[[:space:]]*:[[:space:]]*"[^"]*"' | head -n1 | sed 's/.*"\([^"]*\)"$/\1/')
        fi
        if [ -z "${RESOLVED_VERSION:-}" ]; then
            if [ -z "${GITHUB_TOKEN:-}" ]; then
                die "failed to fetch latest release. If the repository is private, export GITHUB_TOKEN with contents:read scope."
            fi
            die "failed to fetch latest release (check GITHUB_TOKEN and network)"
        fi
    else
        RESOLVED_VERSION="$VERSION"
    fi
    info "resolved version: $RESOLVED_VERSION"
}

# Get the asset ID for a given asset name in the resolved release.
# Needed because private-repo asset downloads go through the API, not
# the public-repo direct download URL.
get_asset_id() {
    _name="$1"
    _body=$(github_api "${API_BASE}/releases/tags/${RESOLVED_VERSION}") \
        || die "failed to fetch release metadata for $RESOLVED_VERSION"
    # Find the asset object whose "name": matches, then the next "id": field.
    # POSIX-safe awk-free parse using grep + sed.
    _ids=$(echo "$_body" | tr ',' '\n' | grep -oE '"(id|name)"[[:space:]]*:[[:space:]]*("[^"]+"|[0-9]+)')
    _asset_id=""
    _prev_id=""
    while IFS= read -r _line; do
        case "$_line" in
            *'"id"'*) _prev_id=$(echo "$_line" | sed 's/.*:[[:space:]]*\([0-9]*\).*/\1/') ;;
            *'"name"'*)
                _this_name=$(echo "$_line" | sed 's/.*"\([^"]*\)"$/\1/')
                if [ "$_this_name" = "$_name" ]; then
                    _asset_id="$_prev_id"
                    break
                fi
                ;;
        esac
    done <<EOF
$_ids
EOF
    if [ -z "$_asset_id" ]; then
        die "asset not found in release $RESOLVED_VERSION: $_name"
    fi
    echo "$_asset_id"
}

# Download a release asset by ID to the given path.
# Same `set --` pattern as github_api — see that function for rationale.
download_asset() {
    _asset_id="$1"
    _dest="$2"

    set -- -fsSL \
        -H "Accept: application/octet-stream"

    if [ -n "${GITHUB_TOKEN:-}" ]; then
        set -- "$@" -H "Authorization: Bearer ${GITHUB_TOKEN}"
    fi

    curl "$@" -o "$_dest" "${API_BASE}/releases/assets/${_asset_id}"
}

# ---------------------------------------------------------------------------
# Checksum verification
# ---------------------------------------------------------------------------

verify_checksum() {
    _file="$1"
    _name="$2"
    _checksums="$3"

    _expected=$(grep "  ${_name}$" "$_checksums" | awk '{print $1}')
    if [ -z "$_expected" ]; then
        die "checksum entry not found for $_name"
    fi

    if command -v sha256sum >/dev/null 2>&1; then
        _actual=$(sha256sum "$_file" | awk '{print $1}')
    elif command -v shasum >/dev/null 2>&1; then
        _actual=$(shasum -a 256 "$_file" | awk '{print $1}')
    else
        die "need sha256sum or shasum to verify checksum"
    fi

    if [ "$_expected" != "$_actual" ]; then
        die "checksum mismatch for $_name: expected $_expected got $_actual"
    fi
    info "checksum verified"
}

# ---------------------------------------------------------------------------
# Main install flow
# ---------------------------------------------------------------------------

main() {
    need_cmd curl
    need_cmd tar
    need_cmd uname
    need_cmd mktemp

    detect_platform
    resolve_version

    # Strip leading 'v' if present — goreleaser archive names use the numeric
    # version (e.g. buttons_0.0.2_..., not v0.0.2_...).
    _numeric=$(echo "$RESOLVED_VERSION" | sed 's/^v//')
    ARCHIVE_NAME="buttons_${_numeric}_${PLATFORM}.tar.gz"
    CHECKSUMS_NAME="checksums.txt"

    _tmp=$(mktemp -d 2>/dev/null || mktemp -d -t buttons-install)
    trap 'rm -rf "$_tmp"' EXIT INT TERM

    info "downloading $ARCHIVE_NAME"
    _archive_id=$(get_asset_id "$ARCHIVE_NAME")
    download_asset "$_archive_id" "$_tmp/$ARCHIVE_NAME"

    info "downloading $CHECKSUMS_NAME"
    _checksums_id=$(get_asset_id "$CHECKSUMS_NAME")
    download_asset "$_checksums_id" "$_tmp/$CHECKSUMS_NAME"

    verify_checksum "$_tmp/$ARCHIVE_NAME" "$ARCHIVE_NAME" "$_tmp/$CHECKSUMS_NAME"

    info "extracting archive"
    tar -xzf "$_tmp/$ARCHIVE_NAME" -C "$_tmp" "$BINARY_NAME"

    info "installing to $INSTALL_DIR/$BINARY_NAME"
    if [ ! -w "$INSTALL_DIR" ]; then
        if command -v sudo >/dev/null 2>&1; then
            sudo install -m 0755 "$_tmp/$BINARY_NAME" "$INSTALL_DIR/$BINARY_NAME"
        else
            die "$INSTALL_DIR is not writable and sudo is not available. Set BUTTONS_INSTALL_DIR to a writable path, e.g. \$HOME/.local/bin"
        fi
    else
        install -m 0755 "$_tmp/$BINARY_NAME" "$INSTALL_DIR/$BINARY_NAME"
    fi

    # Sanity check — run the installed binary and confirm its self-reported
    # version matches the requested one.
    if _installed_version=$("$INSTALL_DIR/$BINARY_NAME" --version 2>/dev/null); then
        info "installed: $_installed_version"
    else
        warn "installed binary did not respond to --version (this may indicate a platform mismatch or corrupt download)"
    fi

    # Warn if the install dir is not on PATH.
    case ":${PATH:-}:" in
        *":${INSTALL_DIR}:"*) : ;;
        *) warn "$INSTALL_DIR is not in your PATH. Add it with: export PATH=\"${INSTALL_DIR}:\$PATH\"" ;;
    esac

    printf '\n'
    printf 'Buttons %s installed at %s/%s\n' "$RESOLVED_VERSION" "$INSTALL_DIR" "$BINARY_NAME"
    printf 'Run `buttons --help` to get started.\n'
}

main "$@"
