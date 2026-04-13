#!/usr/bin/env node
// scripts/bootstrap-npm.mjs — ONE-TIME use.
//
// Problem
// -------
// npm Trusted Publishing must be configured against an existing package. If
// pending-publisher support is not surfaced on your account, you have to
// claim each package name with a conventional publish first.
//
// Solution
// --------
// This script publishes 5 stub packages (@autono/buttons and the 4 platform
// packages) at version 0.0.0-bootstrap under the non-default "bootstrap"
// dist-tag. Nothing resolves to these stubs via `npm install @autono/buttons`
// — they exist only to claim the names so TP can be configured per package.
//
// After running, go to each package's settings on npmjs.com, configure
// Trusted Publishers pointing at autonoco/buttons + auto-release.yml, then
// revoke the one-time token used here. The first real release merged to
// main will publish a proper version via TP.
//
// Usage
// -----
//   export NPM_TOKEN=<one-time Automation token with publish scope on @autono/*>
//   node scripts/bootstrap-npm.mjs
//
// The script writes a temporary .npmrc in dist-npm/ that references the
// token, publishes all 5 packages, then deletes the .npmrc. Revoke the
// token on npmjs.com as soon as the script finishes.

import { execFileSync } from 'node:child_process';
import { chmodSync, mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const SCRIPT_DIR = dirname(fileURLToPath(import.meta.url));
const REPO_ROOT = resolve(SCRIPT_DIR, '..');
const OUT_DIR = join(REPO_ROOT, 'dist-npm');
const NPMRC_PATH = join(OUT_DIR, '.npmrc');

const BOOTSTRAP_VERSION = '0.0.0-bootstrap';
// Explicit non-default dist-tag. Without this, npm would make 0.0.0-bootstrap
// the "latest" of each package, which would send consumers to a stub. Real
// releases publish without --tag, which defaults to "latest" and supersedes
// this tag for normal installs.
const DIST_TAG = 'bootstrap';

const ARCH_MAP = { amd64: 'x64', arm64: 'arm64' };
const TARGETS = [
    { goos: 'darwin', goarch: 'amd64' },
    { goos: 'darwin', goarch: 'arm64' },
    { goos: 'linux', goarch: 'amd64' },
    { goos: 'linux', goarch: 'arm64' },
];

function log(msg) {
    process.stdout.write(`[bootstrap-npm] ${msg}\n`);
}

function die(msg) {
    process.stderr.write(`[bootstrap-npm] error: ${msg}\n`);
    cleanupNpmrc();
    process.exit(1);
}

function setupAuth() {
    const token = process.env.NPM_TOKEN || process.env.NODE_AUTH_TOKEN;
    if (!token) {
        die(
            'NPM_TOKEN (or NODE_AUTH_TOKEN) is required.\n' +
                'Create a one-time Automation token at https://www.npmjs.com/settings/~/tokens\n' +
                'with publish scope on @autono/*. Revoke it as soon as this script finishes.'
        );
    }
    rmSync(OUT_DIR, { recursive: true, force: true });
    mkdirSync(OUT_DIR, { recursive: true });
    // npm picks up .npmrc from the CWD and parent dirs when publishing. A
    // single .npmrc at dist-npm/ covers every stub dir beneath it.
    writeFileSync(NPMRC_PATH, `//registry.npmjs.org/:_authToken=${token}\n`, { mode: 0o600 });
    log(`wrote temporary ${NPMRC_PATH}`);
}

function cleanupNpmrc() {
    try {
        rmSync(NPMRC_PATH, { force: true });
    } catch {
        // nothing to do
    }
}

// Stub placeholder content. If someone accidentally resolves this version
// (they shouldn't — it's on the "bootstrap" dist-tag), the binary/shim
// prints an error explaining they hit the wrong version.
const STUB_BINARY = `#!/bin/sh
echo "buttons: this is a bootstrap stub. Install a real release: npm install -g @autono/buttons" >&2
exit 1
`;

const STUB_SHIM = `#!/usr/bin/env node
process.stderr.write('buttons: this is a bootstrap stub. Install a real release: npm install -g @autono/buttons\\n');
process.exit(1);
`;

function writeStubPlatformPackage({ goos, goarch }) {
    const nodeArch = ARCH_MAP[goarch];
    const pkgName = `@autono/buttons-${goos}-${nodeArch}`;
    const pkgDir = join(OUT_DIR, `bootstrap-buttons-${goos}-${nodeArch}`);
    mkdirSync(join(pkgDir, 'bin'), { recursive: true });

    const pkgJson = {
        name: pkgName,
        version: BOOTSTRAP_VERSION,
        description: 'Bootstrap stub — replaced by real release.',
        license: 'Apache-2.0',
        homepage: 'https://buttons.sh',
        repository: { type: 'git', url: 'git+https://github.com/autonoco/buttons.git' },
        os: [goos],
        cpu: [nodeArch],
        files: ['bin/'],
    };
    writeFileSync(join(pkgDir, 'package.json'), JSON.stringify(pkgJson, null, 2) + '\n');

    const binPath = join(pkgDir, 'bin', 'buttons');
    writeFileSync(binPath, STUB_BINARY);
    chmodSync(binPath, 0o755);

    return { pkgName, pkgDir };
}

function writeStubMainPackage() {
    const pkgDir = join(OUT_DIR, 'bootstrap-buttons');
    mkdirSync(join(pkgDir, 'bin'), { recursive: true });

    const pkgJson = {
        name: '@autono/buttons',
        version: BOOTSTRAP_VERSION,
        description: 'Bootstrap stub — replaced by real release.',
        license: 'Apache-2.0',
        homepage: 'https://buttons.sh',
        repository: { type: 'git', url: 'git+https://github.com/autonoco/buttons.git' },
        bin: { buttons: 'bin/buttons.js' },
        files: ['bin/'],
        engines: { node: '>=18' },
        // Intentionally no optionalDependencies — the stub doesn't need them
        // and omitting keeps `npm install @autono/buttons@0.0.0-bootstrap`
        // from failing on missing stub platform deps.
    };
    writeFileSync(join(pkgDir, 'package.json'), JSON.stringify(pkgJson, null, 2) + '\n');

    const shimPath = join(pkgDir, 'bin', 'buttons.js');
    writeFileSync(shimPath, STUB_SHIM);
    chmodSync(shimPath, 0o755);

    return { pkgName: '@autono/buttons', pkgDir };
}

function publishStub(pkgDir, pkgName) {
    log(`publishing ${pkgName}@${BOOTSTRAP_VERSION} (dist-tag: ${DIST_TAG})`);
    try {
        execFileSync('npm', ['publish', '--access', 'public', '--tag', DIST_TAG], {
            cwd: pkgDir,
            stdio: 'inherit',
        });
    } catch (err) {
        die(`failed to publish ${pkgName}: npm publish exited non-zero`);
    }
}

function main() {
    setupAuth();

    try {
        for (const target of TARGETS) {
            const { pkgName, pkgDir } = writeStubPlatformPackage(target);
            publishStub(pkgDir, pkgName);
        }
        const { pkgName, pkgDir } = writeStubMainPackage();
        publishStub(pkgDir, pkgName);

        log('');
        log('bootstrap complete. Next steps:');
        log('  1. Revoke the NPM_TOKEN you used — go to https://www.npmjs.com/settings/~/tokens');
        log('  2. Configure Trusted Publishers for each of these 5 packages:');
        log('     https://www.npmjs.com/package/@autono/buttons/access');
        log('     https://www.npmjs.com/package/@autono/buttons-darwin-x64/access');
        log('     https://www.npmjs.com/package/@autono/buttons-darwin-arm64/access');
        log('     https://www.npmjs.com/package/@autono/buttons-linux-x64/access');
        log('     https://www.npmjs.com/package/@autono/buttons-linux-arm64/access');
        log('  3. Merge PR #28. Next release to main publishes real versions via TP.');
    } finally {
        cleanupNpmrc();
        log('cleaned up temporary .npmrc');
    }
}

main();
