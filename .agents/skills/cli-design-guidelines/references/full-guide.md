# CLI Design Guidelines — Full Reference

Source: [clig.dev](https://clig.dev/) — An open-source guide to help you write better command-line programs, following traditional UNIX principles while updating them for the modern day.

---

## Philosophy

### Human-first design
Traditional UNIX commands were created assuming primary use by other programs. Today, even when CLIs are used primarily by humans, interaction design often carries baggage from this past. Modern CLIs designed for human use should prioritize human needs over machine-readability as the default behavior.

### Simple parts that work together
Core UNIX philosophy emphasizes small, simple programs with clean interfaces that combine to build larger systems. Rather than stuffing features into single programs, create modular programs for recombination as needed. Pipes, shell scripts, and modern automation (CI/CD, orchestration) still rely on this composability. Standard mechanisms like stdin/stdout/stderr, signals, exit codes, and plain text ensure different programs integrate smoothly. JSON provides additional structure for complex data when needed.

Designing for composability need not conflict with human-first design—both can coexist.

### Consistency across programs
Terminal conventions are deeply learned. Following existing patterns makes CLIs intuitive and guessable, enabling user efficiency. However, consistency sometimes conflicts with usability. When following convention would compromise usability, breaking with it may be appropriate—but this decision requires careful consideration.

### Saying (just) enough
The terminal presents pure information. Output can be too little (program hangs for minutes, users wonder if it's broken) or too much (pages of debugging output drowning important information). Balance is crucial for clarity and user empowerment.

### Ease of discovery
GUIs display everything on screen; CLIs are assumed to require memorization. Yet these needn't be mutually exclusive. Discoverable CLIs offer comprehensive help texts, examples, suggestions for next commands, and error guidance—borrowing GUI principles while maintaining CLI efficiency.

### Conversation as the norm
CLI interaction embodies an accidental metaphor: conversation. Beyond simple commands, programs typically involve multiple invocations. Learning through repeated trial-and-error resembles dialogue with the program. Other conversational patterns include: multi-step setup then learning what to run; sequential commands building toward a final action; exploring systems; dry-running complex operations before execution. Good CLI design acknowledges this conversational nature, allowing you to suggest corrections, clarify intermediate state, and confirm dangerous actions.

### Robustness
Robustness has objective and subjective dimensions. Programs must handle unexpected input gracefully, perform idempotent operations where possible, etc. They should also *feel* robust—appearing solid and responsive rather than fragile. Subjective robustness requires attention to detail: keeping users informed, explaining common errors, avoiding scary stack traces. Simplicity contributes to robustness by reducing special cases and fragile code.

### Empathy
Command-line tools are programmers' creative toolkits and should be enjoyable to use. This means giving users the feeling you're on their side, wanting them to succeed, having carefully considered their problems. Empathy means exceeding expectations at every turn—it's not about emoji or gamification, though these aren't inherently wrong.

### Chaos
The terminal environment contains inconsistencies everywhere. Yet this chaos enables power—the terminal places few constraints on what you can build, fostering invention. This document advises following existing patterns while acknowledging that rule-breaking sometimes becomes necessary. As Jef Raskin wrote: "Abandon a standard when it is demonstrably harmful to productivity or user satisfaction."

---

## The Basics

**Use a command-line argument parsing library.** Either your language's built-in library or a good third-party option. These handle arguments, flag parsing, help text, and spelling suggestions sensibly.

Recommended libraries by language:
- Multi-platform: docopt
- Bash: argbash
- Go: Cobra, cli
- Haskell: optparse-applicative
- Java: picocli
- Julia: ArgParse.jl, Comonicon.jl
- Kotlin: clikt
- Node: oclif
- Deno: parseArgs
- Perl: Getopt::Long
- PHP: console, CLImate
- Python: Argparse, Click, Typer
- Ruby: TTY
- Rust: clap
- Swift: swift-argument-parser

**Return zero exit code on success, non-zero on failure.** Scripts determine program success/failure via exit codes. Report this correctly and map non-zero codes to important failure modes.

**Send output to stdout.** Primary command output goes to stdout. Machine-readable output also goes to stdout (piping's default destination).

**Send messaging to stderr.** Log messages, errors, and related messaging go to stderr. This prevents these messages from being fed into piped commands while displaying them to users.

---

## Help

**Display extensive help text when asked.** Show help when passed `-h` or `--help` flags, including for subcommands with their own help.

**Display concise help text by default.** When a program requires arguments but is run without them, display concise help (excluding programs interactive by default like `npm init`).

Concise help should include only:
- Description of what the program does
- One or two example invocations
- Flag descriptions (unless there are many)
- Instruction to pass `--help` for full information

Example from `jq`:
```
$ jq
jq - commandline JSON processor [version 1.6]

Usage:    jq [options] <jq filter> [file...]
    jq [options] --args <jq filter> [strings...]
    jq [options] --jsonargs <jq filter> [JSON_TEXTS...]

jq is a tool for processing JSON inputs, applying the given filter to
its JSON text inputs and producing the filter's results as JSON on
standard output.

The simplest filter is ., which copies jq's input to its output
unmodified (except for formatting, but note that IEEE754 is used
for number representation internally, with all that that implies).

For more advanced filters see the jq(1) manpage ("man jq")
and/or https://stedolan.github.io/jq

Example:

    $ echo '{"foo": 0}' | jq .
    {
        "foo": 0
    }

For a listing of options, use jq --help.
```

**Show full help when `-h` and `--help` are passed.** All of these should show help:
```
$ myapp
$ myapp --help
$ myapp -h
```

Ignore other flags and arguments—users should be able to add `-h` to anything. Don't overload `-h`.

For `git`-like programs, also support:
```
$ myapp help
$ myapp help subcommand
$ myapp subcommand --help
$ myapp subcommand -h
```

**Provide a support path for feedback and issues.** Include website or GitHub link in top-level help text.

**In help text, link to the web version of documentation.** Link directly to specific pages or anchors for subcommands. This proves especially useful when documentation provides more detail or additional reading.

**Lead with examples.** Users typically prefer examples over other documentation forms. Show them first, particularly for common complex uses. Include actual output if it clarifies behavior without excessive length. Build a narrative with series of examples advancing toward complex uses.

**If you've got loads of examples, put them somewhere else.** Use cheat sheet commands or web pages for exhaustive, advanced examples to avoid overwhelming help text. Consider full tutorials for complex integration use cases.

**Display the most common flags and commands at the start of help text.** Many flags are fine, but highlight really common ones first.

Example from `git`:
```
$ git
usage: git [--version] [--help] [-C <path>] [-c <name>=<value>]
           [--exec-path[=<path>]] [--html-path] [--man-path] [--info-path]
           [-p | --paginate | -P | --no-pager] [--no-replace-objects] [--bare]
           [--git-dir=<path>] [--work-tree=<path>] [--namespace=<name>]
           <command> [<args>]

These are common Git commands used in various situations:

start a working area (see also: git help tutorial)
   clone      Clone a repository into a new directory
   init       Create an empty Git repository or reinitialize an existing one

work on the current change (see also: git help everyday)
   add        Add file contents to the index
   mv         Move or rename a file, a directory, or a symlink
   reset      Reset current HEAD to the specified state
   rm         Remove files from the working tree and from the index

examine the history and state (see also: git help revisions)
   bisect     Use binary search to find the commit that introduced a bug
   grep       Print lines matching a pattern
   log        Show commit logs
   show       Show various types of objects
   status     Show the working tree status
…
```

**Use formatting in your help text.** Bold headings improve scannability, using terminal-independent methods to avoid escape character walls.

**If the user did something wrong and you can guess what they meant, suggest it.** For example, `brew update jq` suggests running `brew upgrade jq`. You can ask for confirmation, but don't auto-correct silently—invalid input doesn't necessarily indicate simple typos, and auto-correcting risks dangerous state changes while preventing users from learning proper syntax.

**If your command expects piped input but stdin is an interactive terminal, display help immediately and quit.** This prevents hanging like `cat` does. Alternatively, print a log message to stderr.

---

## Documentation

Help text provides brief immediate understanding; documentation offers full detail covering what the tool is, what it isn't, how it works, and everything users might need.

**Provide web-based documentation.** Users must be able to search online for your tool's documentation and link others to specific sections.

**Provide terminal-based documentation.** Terminal documentation offers advantages: fast access, version-specific synchronization, offline availability.

**Consider providing man pages.** Many users reflexively check `man mycmd` first. Tools like [ronn](http://rtomayko.github.io/ronn/) generate both man pages and web docs. Since not everyone knows about `man` and it doesn't run on all platforms, ensure terminal docs are accessible through your tool itself (e.g., `git help` and `npm help ls`).

---

## Output

**Human-readable output is paramount.** Humans first, machines second. The most reliable heuristic for human reading is checking whether output streams (stdout/stderr) connect to a TTY.

**Have machine-readable output where it does not impact usability.** Text streams form UNIX's universal interface. Programs output text lines; programs expect text input. This composition enables scripting and improves human usability—users can pipe output to `grep` and expect correct behavior.

**If human-readable output breaks machine-readable output, use `--plain`.** Display plain, tabular text format for integration with tools like `grep` or `awk`. When human-readable formatting breaks record-per-line expectations, provide `--plain` for scripts.

**Display output as formatted JSON if `--json` is passed.** JSON provides more structure than plain text. `jq` is the standard for command-line JSON work with a whole ecosystem of tools. JSON's web prevalence enables piping directly to/from web services using `curl`.

**Display output on success, but keep it brief.** Traditionally, UNIX commands display nothing when nothing's wrong. Printing nothing is rarely ideal for default human interaction. Err toward less output. For no-output preferences in shell scripts, provide `-q` option to suppress non-essential output.

**If you change state, tell the user.** State changes deserve explanation so users model the system's new state—especially when results don't directly map to requests. Example: `git push` shows detailed transfer information.

**Make it easy to see the current state of the system.** When your program performs complex state changes not immediately visible in the filesystem, make state viewing easy. Example: `git status` shows branch, changes, and suggests commands.

**Suggest commands the user should run.** When commands form workflows, suggesting next commands helps learning and feature discovery.

**Actions crossing the program's internal world boundary should usually be explicit.** This includes reading/writing files not explicitly passed as arguments and talking to remote servers.

**Increase information density—with ASCII art!** For example, `ls` displays permissions in scannable ways.

**Use color with intention.** Highlight text for user notice or indicate errors (red). Don't overuse—if everything's different colors, color becomes meaningless.

**Disable color if your program is not in a terminal or the user requested it.** Disable when:
- stdout or stderr is not a TTY (check individually)
- `NO_COLOR` environment variable is set and non-empty
- `TERM` environment variable equals `dumb`
- User passes `--no-color` option
- Consider adding `MYAPP_NO_COLOR` for program-specific disabling

**If stdout is not an interactive terminal, don't display animations.** This prevents progress bars from becoming visual noise in CI logs.

**Use symbols and emoji where it makes things clearer.** Careful application prevents cluttering or appearing toy-like.

**By default, don't output information understandable only by software creators.** Developer-facing info should only display in verbose mode.

**Don't treat stderr like a log file, at least not by default.** Don't print log level labels unless in verbose mode.

**Use a pager (e.g. `less`) if outputting lots of text.** Use pagers only when stdin or stdout is an interactive terminal. Good `less` options: `less -FIRX`.

---

## Errors

**Catch errors and rewrite them for humans.** When expecting errors, catch and rewrite messages usefully. Treat it as conversation guiding the user rightward. Example: "Can't write to file.txt. You might need to make it writable by running 'chmod +w file.txt'."

**Signal-to-noise ratio is crucial.** More irrelevant output lengthens error diagnosis time. Consider grouping multiple same-type errors under single explanatory header.

**Consider where the user will look first.** Place most important information at output end. Red text draws eye attention—use intentionally and sparingly.

**If there is an unexpected or unexplainable error, provide debug and traceback information with bug submission instructions.** Consider writing debug logs to files rather than terminal printing.

**Make it effortless to submit bug reports.** Provide a URL pre-populating as much information as possible.

---

## Arguments and Flags

Terminology:
- **Arguments** (args): Positional parameters. Order often matters.
- **Flags**: Named parameters using hyphens. May include user-specified values. Order generally doesn't matter.

**Prefer flags to args.** More typing, but far clearer. Easier to extend later.

**Have full-length versions of all flags.** Include both `-h` and `--help`. Full versions are useful in scripts.

**Only use one-letter flags for commonly used flags.** Especially at top-level. Avoids polluting short flag namespace.

**Multiple arguments are fine for simple multiple-file actions.** Example: `rm file1.txt file2.txt file3.txt`. Works with globbing.

**If you've got two or more arguments for different things, you're probably doing something wrong.** Exception: Common primary actions where brevity merits memorization (`cp <src> <dest>`).

**Use standard names for flags, if standards exist.** Common flags:
- `-a`/`--all`: All items
- `-d`/`--debug`: Show debugging output
- `-f`/`--force`: Force action (skip confirmation)
- `--json`: Display JSON output
- `-h`/`--help`: Help (should mean help only)
- `-n`/`--dry-run`: Don't run; describe changes
- `--no-input`: Disable prompts
- `-o`/`--output`: Output file
- `-p`/`--port`: Port
- `-q`/`--quiet`: Less output
- `-u`/`--user`: User
- `--version`: Version
- `-v`: Often means verbose or version (consider `-d` for verbose)

**Make the default the right thing for most users.** Most users won't find and remember correct flags.

**Prompt for user input.** If users don't pass arguments/flags, prompt them.

**Never require a prompt.** Always provide flag/argument alternatives. Skip prompting when stdin isn't interactive terminal.

**Confirm before doing anything dangerous.** Prompt for `y`/`yes` when interactive, or require `-f`/`--force` otherwise. Risk levels:
- **Mild**: Small local change. May or may not need prompting.
- **Moderate**: Larger local or remote changes. Usually warrant confirmation. Consider dry run.
- **Severe**: Deleting complex items. Require typing the item's name. Support `--confirm="name"` for scripts.

**If input or output is a file, support `-` for stdin/stdout.** Enables piping without temporary files.

**If a flag accepts optional values, allow a special word like "none".** Example: `ssh -F none` runs with no config.

**If possible, make arguments, flags, and subcommands order-independent.** Users add flags at the end after up-arrow recall.

**Do not read secrets directly from flags.** Flag values leak into `ps` output and shell history. Accept secrets only via files (`--password-file`) or stdin.

---

## Interactivity

**Only use prompts or interactive elements if stdin is an interactive terminal (TTY).** Throw errors specifying flag passage when required.

**If `--no-input` is passed, don't prompt or do anything interactive.** Fail telling users how to pass information as flags.

**If you're prompting for a password, don't print it as users type.** Disable terminal echo.

**Let the user escape.** Make exit methods clear. Ensure Ctrl-C still works during network I/O hangs.

---

## Subcommands

**Be consistent across subcommands.** Same flag names for same things; similar output formatting.

**Use consistent names for multiple subcommand levels.** Two-level commands: one noun, one verb. Either `noun verb` or `verb noun`; `noun verb` is more common. Example: `docker container create`.

**Don't have ambiguous or similarly-named commands.** "Update" and "upgrade" confuse users.

**Don't have a catch-all subcommand.** You can never add a subcommand with a name that was previously used as an argument to the catch-all. Scripts break silently.

**Don't allow arbitrary subcommand abbreviations.** Explicit aliases are fine, but implicit prefix-matching prevents future additions.

---

## Robustness

**Validate user input.** Check early and bail before anything breaks, making errors understandable.

**Responsive is more important than fast.** Print something within 100ms. For network requests, print before executing.

**Show progress if something takes a long time.** Good libraries: tqdm (Python), schollz/progressbar (Go), node-progress (Node.js).

**Do stuff in parallel where you can, but be thoughtful about it.** Ensure robustness and non-confusing output interleaving. Use libraries. If progress bars are hidden during normal operation, still print logs when errors occur.

**Make things time out.** Allow network timeout configuration with sensible defaults.

**Make it recoverable.** Transient failures should allow resuming from where it left off.

**Make it crash-only.** Avoid post-operation cleanup or defer cleanup to next run.

**People are going to misuse your program.** Prepare for script wrapping, poor connections, multiple simultaneous instances, unexpected environments.

---

## Future-proofing

**Keep changes additive where you can.** Add new flags rather than changing existing behavior.

**Warn before making non-additive changes.** When users pass deprecated flags, tell them. Detect migration and stop warning.

**Changing output for humans is usually OK.** Encourage script users to use `--plain` or `--json` for stability.

**Don't have a catch-all subcommand.** Prevents adding new commands forever.

**Don't allow arbitrary subcommand abbreviations.** Locks you into never adding conflicting commands.

**Don't create a "time bomb."** Will your command work identically in twenty years?

---

## Signals and Control Characters

**If a user hits Ctrl-C (INT signal), exit as soon as possible.** Say something immediately before cleanup. Add cleanup timeouts.

**If a user hits Ctrl-C during long cleanup operations, skip them.** Tell users what happens when pressing Ctrl-C again.

---

## Configuration

Configuration precedence (highest to lowest):
1. Flags
2. Running shell's environment variables
3. Project-level configuration (e.g., `.env`)
4. User-level configuration
5. System-wide configuration

**Follow the XDG spec.** Use `~/.config/myapp/` instead of littering home directory with dotfiles.

**If you automatically modify non-your-program configuration, ask user consent.**

---

## Environment Variables

**For portability, names must contain only uppercase letters, numbers, underscores.** Don't start with numbers.

**Aim for single-line values.** Multi-line values create usability issues.

**Check general-purpose environment variables:**
- `NO_COLOR` / `FORCE_COLOR`: Color control
- `DEBUG`: Enable verbose output
- `EDITOR`: For file editing prompts
- `HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY`, `NO_PROXY`: Network operations
- `SHELL`: User's preferred shell
- `TERM`, `TERMINFO`, `TERMCAP`: Terminal capabilities
- `TMPDIR`: Temporary files
- `HOME`: Configuration files
- `PAGER`: Auto-paging output
- `LINES`, `COLUMNS`: Screen-size-dependent output

**Read `.env` where appropriate.** But don't use it as a substitute for proper config files.

**Do not read secrets from environment variables.** They leak into child processes, logs, docker inspect, and systemctl show.

---

## Naming

**Make it a simple, memorable word.** Not too generic (avoid conflicts).

**Use only lowercase letters, dashes if really needed.**

**Keep it short.** Users type it constantly.

**Make it easy to type.** Consider hand ergonomics.

---

## Distribution

**If possible, distribute as a single binary.** Use PyInstaller etc. for interpreted languages.

**Make it easy to uninstall.** Document uninstallation at the bottom of install instructions.

---

## Analytics

**Do not phone home usage or crash data without consent.** Explain data collection, reasons, anonymity, and retention.

**Consider analytics alternatives:** Instrument web docs, track downloads, talk to users directly.
