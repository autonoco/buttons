#!/usr/bin/env node
// scripts/publish-npm.mjs — publish Buttons to npm after goreleaser runs.
//
// Invariants
// ----------
// - Goreleaser has already built binaries for darwin/linux × amd64/arm64 and
//   written them into dist/ plus a dist/artifacts.json index.
// - The host has `npm` on PATH and a valid auth token (NODE_AUTH_TOKEN or an
//   .npmrc with //registry.npmjs.org/:_authToken=...).
// - VERSION env var is the bare semver (e.g. "0.11.0", no leading "v").
//
// What this script does
// ---------------------
// 1. Reads dist/artifacts.json, filters to the four platform binaries.
// 2. For each binary, creates dist-npm/buttons-<os>-<arch>/ with a package.json
//    (os+cpu fields scoped to that platform) and bin/buttons. Publishes it.
// 3. Rewrites npm/package.json to stamp the real version on the main package
//    and each optionalDependency. Publishes the main package last.
//
// Why platform packages must publish FIRST: the main package's
// optionalDependencies point at exact versions of the platform packages, and
// npm will hard-fail at install time if a listed optional dep doesn't resolve
// to a published version. Publishing the main package before the platform
// packages would leave a broken window where installs fail.
//
// Dry run
// -------
// Pass --dry-run (or set BUTTONS_NPM_DRY_RUN=1) to generate all dist-npm/
// tarballs without calling `npm publish`. Useful for local verification.

import { execFileSync } from 'node:child_process';
import { copyFileSync, chmodSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const SCRIPT_DIR = dirname(fileURLToPath(import.meta.url));
const REPO_ROOT = resolve(SCRIPT_DIR, '..');
const DIST_DIR = join(REPO_ROOT, 'dist');
const NPM_SRC_DIR = join(REPO_ROOT, 'npm');
const OUT_DIR = join(REPO_ROOT, 'dist-npm');

const DRY_RUN = process.argv.includes('--dry-run') || process.env.BUTTONS_NPM_DRY_RUN === '1';

// goarch → node process.arch. Keep this map in lockstep with the shim's
// resolver in npm/bin/buttons.js.
const ARCH_MAP = {
    amd64: 'x64',
    arm64: 'arm64',
};

// Which (goos, goarch) pairs we publish. Edit in one place when Windows or
// a new arch is added to .goreleaser.yaml.
const TARGETS = [
    { goos: 'darwin', goarch: 'amd64' },
    { goos: 'darwin', goarch: 'arm64' },
    { goos: 'linux', goarch: 'amd64' },
    { goos: 'linux', goarch: 'arm64' },
];

function log(msg) {
    process.stdout.write(`[publish-npm] ${msg}\n`);
}

function die(msg) {
    process.stderr.write(`[publish-npm] error: ${msg}\n`);
    process.exit(1);
}

function getVersion() {
    const raw = process.env.VERSION;
    if (!raw) die('VERSION env var is required (e.g. VERSION=0.11.0)');
    const v = raw.replace(/^v/, '');
    if (!/^\d+\.\d+\.\d+(-[\w.]+)?$/.test(v)) {
        die(`VERSION does not look like semver: "${raw}"`);
    }
    return v;
}

// Skip the publish when no auth token is available (e.g. the NPM_TOKEN secret
// hasn't been set up on the repo yet). We intentionally exit 0 so the first
// release after this feature lands still ships to GitHub + Docker. Once the
// secret is added, subsequent releases publish normally.
function hasNpmAuth() {
    return Boolean(
        process.env.NODE_AUTH_TOKEN || process.env.NPM_TOKEN || process.env.NPM_AUTH_TOKEN
    );
}

function loadArtifacts() {
    const artifactsPath = join(DIST_DIR, 'artifacts.json');
    let raw;
    try {
        raw = readFileSync(artifactsPath, 'utf8');
    } catch (err) {
        die(`cannot read ${artifactsPath}: ${err.message}. Did goreleaser run?`);
    }
    try {
        return JSON.parse(raw);
    } catch (err) {
        die(`artifacts.json is not valid JSON: ${err.message}`);
    }
}

// Goreleaser tags every artifact with `type`. For binaries produced by the
// `builds:` section it uses "Binary". Filter to only those entries that match
// our target list — there's one Binary per (os, arch, amd64-variant).
function findBinary(artifacts, goos, goarch) {
    const matches = artifacts.filter(
        (a) => a.type === 'Binary' && a.goos === goos && a.goarch === goarch
    );
    if (matches.length === 0) {
        die(`no Binary artifact found for ${goos}/${goarch} in dist/artifacts.json`);
    }
    // amd64 builds can have multiple GOAMD64 variants (v1/v2/v3). We only
    // ever build v1 (goreleaser default), so there should be exactly one.
    if (matches.length > 1) {
        const paths = matches.map((m) => m.path).join(', ');
        die(`expected exactly one Binary for ${goos}/${goarch}, found ${matches.length}: ${paths}`);
    }
    const entry = matches[0];
    const absPath = join(REPO_ROOT, entry.path);
    return absPath;
}

function writePlatformPackage({ goos, goarch, version, binarySrc }) {
    const nodeArch = ARCH_MAP[goarch];
    if (!nodeArch) die(`no node arch mapping for goarch "${goarch}"`);

    const pkgName = `@autonoco/buttons-${goos}-${nodeArch}`;
    const pkgDir = join(OUT_DIR, `buttons-${goos}-${nodeArch}`);
    mkdirSync(join(pkgDir, 'bin'), { recursive: true });

    const pkgJson = {
        name: pkgName,
        version,
        description: `Buttons CLI native binary for ${goos}-${nodeArch}.`,
        license: 'Apache-2.0',
        homepage: 'https://buttons.sh',
        repository: {
            type: 'git',
            url: 'git+https://github.com/autonoco/buttons.git',
        },
        bugs: {
            url: 'https://github.com/autonoco/buttons/issues',
        },
        author: 'Autono',
        // os / cpu gate which hosts will install this package via the main
        // package's optionalDependencies. npm / pnpm / bun all honor these.
        os: [goos],
        cpu: [nodeArch],
        files: ['bin/'],
    };
    writeFileSync(join(pkgDir, 'package.json'), JSON.stringify(pkgJson, null, 2) + '\n');

    const binDst = join(pkgDir, 'bin', 'buttons');
    copyFileSync(binarySrc, binDst);
    // The binary must be executable when extracted on the user's machine.
    // npm preserves the mode bits of files inside the tarball.
    chmodSync(binDst, 0o755);

    log(`prepared ${pkgName}@${version} (${binarySrc})`);
    return { pkgName, pkgDir };
}

function writeMainPackage(version, platformPkgNames) {
    const srcPkgPath = join(NPM_SRC_DIR, 'package.json');
    const srcPkg = JSON.parse(readFileSync(srcPkgPath, 'utf8'));

    const outDir = join(OUT_DIR, 'buttons');
    mkdirSync(join(outDir, 'bin'), { recursive: true });

    // Stamp the main version and pin each optional dep to the exact version
    // we just published. Exact pins (no ^ / ~) are required: the shim uses
    // require.resolve() to find the platform package, and any drift between
    // main and platform versions would produce a confusing install error
    // instead of a clear "no prebuilt binary available" message.
    srcPkg.version = version;
    const optDeps = {};
    for (const name of platformPkgNames) {
        optDeps[name] = version;
    }
    srcPkg.optionalDependencies = optDeps;

    writeFileSync(join(outDir, 'package.json'), JSON.stringify(srcPkg, null, 2) + '\n');

    // The `files` field in package.json already restricts the published
    // tarball to bin/, but we still need bin/buttons.js physically present
    // in outDir for `npm publish` to find it.
    copyFileSync(join(NPM_SRC_DIR, 'bin', 'buttons.js'), join(outDir, 'bin', 'buttons.js'));
    chmodSync(join(outDir, 'bin', 'buttons.js'), 0o755);
    copyFileSync(join(NPM_SRC_DIR, 'README.md'), join(outDir, 'README.md'));

    log(`prepared @autonoco/buttons@${version} with ${platformPkgNames.length} optional deps`);
    return outDir;
}

function npmPublish(pkgDir) {
    if (DRY_RUN) {
        log(`DRY RUN: would publish ${pkgDir}`);
        // Run `npm pack` instead so we at least verify the tarball is well-formed.
        execFileSync('npm', ['pack', '--silent'], { cwd: pkgDir, stdio: 'inherit' });
        return;
    }
    log(`publishing ${pkgDir}`);
    execFileSync('npm', ['publish', '--access', 'public'], { cwd: pkgDir, stdio: 'inherit' });
}

function main() {
    const version = getVersion();
    log(`version: ${version}`);
    log(`dry run: ${DRY_RUN}`);

    if (!DRY_RUN && !hasNpmAuth()) {
        log('warning: no npm auth token found (NODE_AUTH_TOKEN / NPM_TOKEN).');
        log('skipping npm publish — GitHub + Docker artifacts already shipped.');
        log('to enable: add an "Automation" token from npmjs.com as a repo secret named NPM_TOKEN.');
        return;
    }

    // Clean slate — previous CI runs or local experiments may have left
    // stale output behind. dist-npm/ is not checked in and holds only
    // generated packages, so rm -rf is safe.
    rmSync(OUT_DIR, { recursive: true, force: true });
    mkdirSync(OUT_DIR, { recursive: true });

    const artifacts = loadArtifacts();

    // Step 1: prepare and publish each platform package.
    const platformPkgNames = [];
    for (const target of TARGETS) {
        const binarySrc = findBinary(artifacts, target.goos, target.goarch);
        const { pkgName, pkgDir } = writePlatformPackage({
            goos: target.goos,
            goarch: target.goarch,
            version,
            binarySrc,
        });
        npmPublish(pkgDir);
        platformPkgNames.push(pkgName);
    }

    // Step 2: prepare and publish the main package (which depends on the
    // platform packages we just pushed).
    const mainDir = writeMainPackage(version, platformPkgNames);
    npmPublish(mainDir);

    if (DRY_RUN) {
        log(`dry-run complete. ${platformPkgNames.length + 1} tarballs packed in dist-npm/.`);
    } else {
        log(`done. @autonoco/buttons@${version} is live on npm.`);
    }
}

main();
