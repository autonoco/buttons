#!/usr/bin/env node
// Buttons CLI launcher.
//
// This package (@autonoco/buttons) is a thin JS shim. The real CLI is a Go
// binary shipped in per-platform optional dependencies. When npm / pnpm / bun
// install @autonoco/buttons, they also install exactly one matching platform
// package — e.g. @autonoco/buttons-darwin-arm64 on Apple Silicon — because
// each platform package declares `os` and `cpu` fields. Unmatched platforms
// are skipped silently (that's the whole point of `optionalDependencies`).
//
// This shim locates the platform package's binary and execs it with the same
// argv, stdio, exit code, and signal that the user invoked us with.

'use strict';

const path = require('path');
const { spawnSync } = require('child_process');

const { platform, arch } = process;
const pkgName = `@autonoco/buttons-${platform}-${arch}`;
const binName = platform === 'win32' ? 'buttons.exe' : 'buttons';

let binary;
try {
    // Resolve the platform package's package.json rather than the binary
    // directly — require.resolve() honors Node's module resolution for JSON
    // and metadata, but is unreliable for non-JS files across package
    // managers. Joining from the package.json's dirname is portable.
    const pkgJson = require.resolve(`${pkgName}/package.json`);
    binary = path.join(path.dirname(pkgJson), 'bin', binName);
} catch (err) {
    process.stderr.write(
        `buttons: no prebuilt binary available for ${platform}-${arch}.\n` +
            `Expected optional dependency "${pkgName}" to be installed.\n\n` +
            `Supported platforms:\n` +
            `  - darwin-arm64 (Apple Silicon)\n` +
            `  - darwin-x64   (Intel Mac)\n` +
            `  - linux-arm64  (ARM servers, Raspberry Pi 4+)\n` +
            `  - linux-x64    (most Linux + WSL)\n\n` +
            `If your platform is supported, re-run the install and confirm\n` +
            `the platform package was not skipped by --no-optional.\n`
    );
    process.exit(1);
}

// Use spawnSync so we inherit stdio (including a TTY) exactly and the Node
// process stays alive long enough to forward the child's exit status. The
// Node wrapper adds ~20-40ms of startup overhead — unavoidable unless we
// shell out to exec(2) directly, which isn't portable to Windows.
const result = spawnSync(binary, process.argv.slice(2), { stdio: 'inherit' });

if (result.error) {
    process.stderr.write(`buttons: failed to launch ${binary}: ${result.error.message}\n`);
    process.exit(1);
}

if (result.signal) {
    // Propagate the signal to ourselves so shells see the expected "killed by
    // SIGINT" exit semantics (e.g. exit 130 on Ctrl-C).
    process.kill(process.pid, result.signal);
} else {
    process.exit(result.status != null ? result.status : 0);
}
