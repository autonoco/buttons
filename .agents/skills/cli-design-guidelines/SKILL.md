---
name: cli-design-guidelines
description: >
    Design human-first CLI tools following clig.dev best practices. Covers help text, output formatting,
    error handling, flags/args, subcommands, interactivity, configuration, signals, naming, and distribution.

    Use when: building CLI tools, designing command interfaces, adding flags or subcommands,
    writing help text, formatting CLI output, handling CLI errors, or reviewing CLI UX.
---

Apply these rules when building or reviewing CLI tools. For full rationale and examples, see [full-guide.md](references/full-guide.md).

## Core Philosophy

- **Human-first**: Design for humans by default, machines via flags (`--json`, `--plain`).
- **Composable**: stdout for data, stderr for messaging. Support pipes. Use exit codes correctly.
- **Consistent**: Follow established conventions. Users guess based on other CLIs.
- **Discoverable**: Suggest next commands, show examples in help, guide on errors.
- **Empathetic**: Keep users informed, explain errors helpfully, confirm dangerous actions.

---

## Help Text

- Show concise help when run with no args: description, 1-2 examples, common flags, pointer to `--help`.
- Show full help on `-h` and `--help`. Both must work. Don't overload `-h`.
- For subcommand CLIs, also support `myapp help <subcmd>` and `myapp <subcmd> --help`.
- Lead with examples. Build a narrative from simple to complex.
- Show the most common flags and commands first, group by purpose.
- Use formatting (bold headings, whitespace) for scannability.
- Link to web docs from help text. Provide a support/issues URL.
- If the user made a typo, suggest the correct command. Ask for confirmation before executing it.
- If expecting piped stdin but running in a TTY, show help and quit (don't hang).

## Output

- **stdout** = program output (data). **stderr** = messages, logs, errors.
- Detect TTY: human-friendly output for terminals, machine-friendly for pipes.
- Support `--json` for structured JSON output. Support `--plain` for grep-friendly tabular output.
- Support `-q`/`--quiet` to suppress non-essential output.
- Print something on success, but keep it brief. Don't print nothing.
- When changing state, tell the user what changed.
- Suggest commands the user should run next (like `git status` does).
- Use a pager for long output. Good `less` flags: `-FIRX`.
- Don't print log-level labels (ERR, WARN) or developer-facing info by default. Reserve for `-v`/`--verbose`.
- Disable animations when not in a TTY (CI logs).

### Color

- Use color with intention: highlight important info, red for errors. Don't rainbow everything.
- Disable color when: stdout/stderr is not a TTY, `NO_COLOR` is set, `TERM=dumb`, or `--no-color` is passed.
- Check stdout and stderr independently (piping one shouldn't disable color on the other).

### Symbols and Density

- Use ASCII art, symbols, and emoji where they genuinely clarify. Don't overdo it.
- Increase information density when it aids scanning (like `ls -l` permission columns).

## Errors

- Catch expected errors and rewrite them for humans. Include what went wrong AND how to fix it.
- Keep signal-to-noise ratio high. Group similar errors under one header.
- Put the most important information last (where the eye lands).
- Use red sparingly and intentionally.
- For unexpected errors: provide debug info and a bug-report URL (pre-populated if possible).
- Write debug/traceback to a file rather than flooding the terminal.

## Arguments and Flags

- **Prefer flags to args.** More typing, much clearer. Easier to extend later.
- Every flag gets a `--long-form`. One-letter shortcuts only for the most common flags.
- Standard flag names (use these, don't reinvent):
  - `-a`/`--all`, `-d`/`--debug`, `-f`/`--force`, `-h`/`--help`
  - `-n`/`--dry-run`, `-o`/`--output`, `-p`/`--port`, `-q`/`--quiet`
  - `-u`/`--user`, `-v`/`--version`, `--json`, `--no-input`
- Multiple args are fine for the same kind of thing (`rm file1 file2`). Two args for *different* things is usually wrong (exception: `cp src dest`).
- Make flag/arg/subcommand order independent when possible.
- Support `-` for stdin/stdout (e.g., `tar xvf -`).
- **Never read secrets from flags.** They leak into `ps` and shell history. Use `--password-file` or stdin.
- Make the default the right thing for most users.

## Subcommands

- Be consistent across subcommands: same flag names, same output formatting.
- Use consistent naming for multi-level commands: `noun verb` or `verb noun` (pick one).
- Don't have ambiguous names ("update" vs "upgrade").
- Don't have a catch-all subcommand (prevents adding new commands later).
- Don't allow arbitrary abbreviations of subcommand names (locks you in).

## Interactivity

- Only prompt if stdin is a TTY. In non-TTY contexts, fail with instructions on which flags to pass.
- Support `--no-input` to explicitly disable all prompts.
- Don't echo passwords. Use terminal echo-disable helpers.
- Let users escape: Ctrl-C must always work. Clarify exit methods in wrappers.

### Confirming Dangerous Actions

- **Mild risk** (delete a file): Optional confirmation or just do it if the command is explicitly destructive.
- **Moderate risk** (delete a directory, remote resource): Prompt for confirmation. Consider `--dry-run`.
- **Severe risk** (delete an entire deployment): Require typing the resource name. Support `--confirm="name"` for scripts.
- Watch for non-obvious destruction (changing a count from 10 to 1 implicitly deletes 9 items).

## Configuration

Precedence (highest to lowest): flags > env vars > project config (`.env`) > user config > system config.

- **Per-invocation** (debug level, dry-run): flags + env vars.
- **Per-machine** (paths, proxy, color): flags + env vars. Maybe config file if complex.
- **Per-project, all users** (build config): version-controlled config files.
- Follow XDG Base Directory spec (`~/.config/myapp/`). Don't litter `$HOME` with dotfiles.
- Don't auto-modify other programs' config without consent.

## Environment Variables

- Uppercase letters, numbers, underscores only. Don't start with a number.
- Single-line values preferred.
- Check standard vars: `NO_COLOR`, `DEBUG`, `EDITOR`, `HTTP_PROXY`, `SHELL`, `TERM`, `TMPDIR`, `HOME`, `PAGER`, `LINES`, `COLUMNS`.
- Read `.env` files for project-specific config, but don't use `.env` as a substitute for proper config files.
- **Never read secrets from env vars.** They leak into child processes, logs, `docker inspect`, `systemctl show`. Use credential files, pipes, or secret managers.

## Signals

- On Ctrl-C (SIGINT): print a message immediately, then exit as fast as possible. Timeout any cleanup.
- On second Ctrl-C during cleanup: skip cleanup and exit immediately. Warn about consequences.
- Design for crash-only: avoid post-op cleanup, defer to next run. Makes programs robust and responsive.

## Robustness

- Validate input early, fail with clear errors before doing anything.
- Print something within 100ms. For network requests, print before the request.
- Show progress for long operations. Use animated indicators so users know it's not hung.
- Make timeouts configurable with sensible defaults.
- Make operations resumable after transient failures (up-arrow + enter should work).
- Make operations idempotent where possible.
- Anticipate misuse: script wrapping, bad connections, concurrent instances, weird filesystems.

## Naming

- Simple, memorable, lowercase word. Dashes if needed, no capitals.
- Keep it short but not cryptic. Easy to type (consider hand ergonomics).
- Don't conflict with existing common commands.

## Distribution

- Distribute as a single binary when possible. Use PyInstaller etc. for interpreted languages.
- Make uninstallation easy and documented.

## Analytics

- Never phone home without explicit consent.
- Prefer alternatives: instrument web docs, track downloads, talk to users directly.

## Exit Codes

- `0` = success. Non-zero = failure.
- Map distinct non-zero codes to important failure modes.

## Future-proofing

- Keep changes additive (new flags, not changed flag behavior).
- Warn users in-program before making breaking changes. Detect when they've migrated and stop warning.
- Human-readable output can change freely. Machine-readable output (`--json`, `--plain`) is a contract.
