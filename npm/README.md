# @autonoco/buttons

**n8n for agents.** A CLI workflow engine where AI agents build and run their own automations.

Each button is a self-contained, reusable action. Create it once, press it forever. Buttons wraps code, APIs, and agent instructions into a single interface with typed args and structured output.

## Install

```bash
# npm
npm install -g @autonoco/buttons

# pnpm
pnpm add -g @autonoco/buttons

# bun
bun add -g @autonoco/buttons
```

After install, the `buttons` binary is on your `$PATH`.

## Quick start

```bash
# Scaffold a shell button. Edit the generated main.sh, then press it.
buttons create deploy --arg env:string:required
buttons press deploy --arg env=staging

# Or provide the code inline:
buttons create now --code 'date +"%Y-%m-%d %H:%M:%S %Z"'
buttons press now

# Wrap an HTTP endpoint:
buttons create weather --url 'https://wttr.in/NYC?format=3'
buttons press weather
```

Run `buttons --help` to see every command.

## How this package works

This npm package is a thin JS shim. The real CLI is a Go binary — the same one shipped via the GitHub Releases tarballs and the Homebrew tap. The shim resolves the matching platform package at runtime and execs the binary.

Platform packages (installed automatically via `optionalDependencies`):

- `@autonoco/buttons-darwin-arm64` — Apple Silicon
- `@autonoco/buttons-darwin-x64` — Intel Mac
- `@autonoco/buttons-linux-arm64` — ARM servers, Raspberry Pi 4+
- `@autonoco/buttons-linux-x64` — most Linux + WSL

Windows is tracked separately; see [autonoco/autono#350](https://github.com/autonoco/autono/issues/350).

## Alternative installs

If you don't want the Node wrapper overhead (~30ms per invocation), install the native binary directly:

```bash
# Universal installer
curl -fsSL https://raw.githubusercontent.com/autonoco/buttons/main/install.sh | sh

# Go toolchain
go install github.com/autonoco/buttons@latest
```

See the [GitHub repo](https://github.com/autonoco/buttons) for Docker, Homebrew, and full documentation.

## License

Apache-2.0 — see [LICENSE](https://github.com/autonoco/buttons/blob/main/LICENSE).
