# Security Policy

## Supported Versions

Buttons is pre-1.0 and under active development. Only the latest release receives security fixes.

| Version | Supported |
|---------|-----------|
| 0.x     | ✅        |

## Reporting a Vulnerability

**Please do not open a public GitHub issue for security reports.**

Report privately via [GitHub Security Advisories](https://github.com/autonoco/buttons/security/advisories/new):

1. Go to the **Security** tab of this repository.
2. Click **Report a vulnerability**.
3. Fill out the form with as much detail as you can (repro steps, affected versions, impact).

If GitHub Security Advisories are unavailable to you, email **bobak@autono.co** directly. Include the word `SECURITY` in the subject line.

### Response SLA

- **Acknowledgment:** within 3 business days
- **Initial triage:** within 7 business days
- **Patch or mitigation:** within 30 days for confirmed high-severity issues

We'll credit reporters in release notes unless they request otherwise.

## Threat Model

Buttons is a CLI workflow engine that **executes user-defined code** on the user's machine. By design, a button can run arbitrary shell scripts, Python, Node, or HTTP requests. **Users are responsible for the buttons they create and install.** Buttons is not a sandbox — it's an orchestrator.

### In scope

The following are security bugs we want to hear about:

- **Path traversal** through button names, arg values, or file/code paths
- **Command injection** through argument values into the executed script/shell
- **Permission escalation** — anything that causes Buttons to run with more privilege than the invoking user
- **Credential leakage** — secrets from `BUTTONS_ARG_*` env vars or `~/.buttons/` leaking into logs, history, or error messages
- **File permission integrity** on `~/.buttons/` (spec/history should stay `0600`, data dirs `0700`)
- **Race conditions** in concurrent button execution that corrupt history or spec files
- **Timeout bypass** — scripts that evade the `context.WithTimeout` + SIGTERM/SIGKILL kill chain

### Assumptions (not bugs)

Buttons inherits these properties from its environment. Issues in this list are working as intended:

- **`$PATH` is trusted.** Buttons resolves runtimes (`python3`, `node`) via `exec.LookPath`, which searches `$PATH`. An attacker who can control the user's `$PATH` can substitute a malicious binary. This is the standard Unix threat model — any CLI that shells out inherits it. Run Buttons in environments where `$PATH` is trusted.

- **`~/.buttons/` is user-private (mode `0700`).** Buttons creates the data directory and all subdirectories with `0700` permissions. If a user changes these or mounts the directory on a shared filesystem, other local users can tamper with buttons, plant symlinks, or inject code. Don't do that.

- **The user trusts the buttons they install.** `buttons store install` (Phase 3) will execute arbitrary scripts from a third-party registry. Verify the source before installing. Signature verification is planned but not yet shipped.

- **`BUTTONS_HOME` is writable only by the user.** If you override the data directory via `BUTTONS_HOME`, the target must be user-private. Buttons does not verify permissions of a user-supplied directory on every operation.

### Known limitations (deferred to later phases)

These are documented gaps we'll address before the relevant feature ships:

- **SSRF in HTTP buttons.** `buttons press` with an HTTP button does not block requests to private IP ranges (`127.0.0.0/8`, `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `169.254.0.0/16`, link-local IPv6). Safe in Phase 1 because the user creates their own buttons on their own machine — SSRF offers nothing beyond what the user could already do with `curl`. **Will be addressed before `buttons serve` (REST API) and `buttons mcp` (MCP server) ship.** Tracked in the project backlog.

- **No sandboxing.** Buttons runs code with the user's full privileges. No containerization, no seccomp, no namespaces. Process-level isolation is under discussion for Phase 4+.

- **No supply chain verification for the Store.** Phase 3 registry installs will execute untrusted code until signature verification lands.

### Out of scope

The following are not security issues and will be closed:

- Vulnerabilities requiring pre-existing local root/admin access
- Social engineering of users into creating malicious buttons
- Rate limiting on local CLI invocations (not a network service in Phase 1)
- Resource exhaustion from an attacker the user has already given command-execution access to (they already have that)
- Side-channel attacks on the local machine (CPU cache, electromagnetic, etc.)

## Disclosure

We follow coordinated disclosure. If you report an issue, please give us reasonable time to investigate and ship a fix before disclosing publicly. We commit to the SLA above.
