package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/autonoco/buttons/internal/agentskill"
	"github.com/autonoco/buttons/internal/config"
)

// initAgents is the --agent flag. Comma-separated list of target IDs.
// Special values: "none" disables agent skill installation entirely.
// When unset (nil), the command decides interactively vs no-op based
// on jsonOutput / noInput / TTY.
var initAgents []string

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a project-local .buttons directory",
	Long: `Create a .buttons/ directory in the current working directory so
buttons are scoped to this project instead of the global ~/.buttons/.

Project-local buttons are discovered automatically: any buttons
command run inside this directory (or a subdirectory) will use the
local .buttons/ folder. Buttons created here won't appear when you
run commands from other projects.

The global ~/.buttons/ is still used as a fallback when no project-
local .buttons/ exists in the directory tree.

A .gitignore is created inside .buttons/ to exclude run history
(pressed/) from version control while keeping button specs, code
files, and agent instructions committed.

A reference guide is written to .buttons/AGENT.md so any coding
agent that opens the folder can learn what Buttons is and how to
use it.

Interactively (on a TTY), init also offers to install a Buttons
skill file for your coding agent (Cursor, Claude Code, Cline,
GitHub Copilot, or a generic AGENTS.md). None is installed without
explicit selection.

Examples:
  cd my-project
  buttons init
  buttons init --agent cursor,agents-md   # non-interactive selection
  buttons init --agent none                # skip the skill picker
  buttons create deploy --code './scripts/deploy.sh' --arg env:string:required`,
	Args: cobra.NoArgs,
	RunE: runInit,
}

func runInit(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cannot determine working directory: %w", err)
	}

	buttonsDir := filepath.Join(cwd, ".buttons")
	alreadyExisted := dirExists(buttonsDir)

	if err := ensureButtonsDir(buttonsDir); err != nil {
		return err
	}

	// The AGENT.md reference is always (re)written — it's Buttons' own
	// docs, not something the user should hand-edit in-place.
	agentMDPath, err := agentskill.WriteAgentMD(cwd)
	if err != nil {
		return err
	}

	// Decide which agent targets to install.
	targets, err := resolveAgentTargets(cmd)
	if err != nil {
		return err
	}

	var installResults []agentskill.WriteResult
	if len(targets) > 0 {
		installResults, err = agentskill.Install(agentskill.InstallOpts{
			ProjectRoot: cwd,
			TargetIDs:   targets,
		})
		if err != nil {
			return err
		}
	}

	if jsonOutput {
		return config.WriteJSON(map[string]any{
			"path":           buttonsDir,
			"created":        !alreadyExisted,
			"agent_md":       agentMDPath,
			"agent_installs": installResults,
		})
	}

	return printInitSummary(cwd, alreadyExisted, agentMDPath, installResults)
}

// ensureButtonsDir creates the .buttons/ tree if it doesn't exist.
// Idempotent: running init twice is a no-op on the directory side.
func ensureButtonsDir(buttonsDir string) error {
	dirs := []string{
		buttonsDir,
		filepath.Join(buttonsDir, "buttons"),
		filepath.Join(buttonsDir, "drawers"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0700); err != nil {
			return fmt.Errorf("failed to create %s: %w", d, err)
		}
	}

	gitignorePath := filepath.Join(buttonsDir, ".gitignore")
	if !fileExists(gitignorePath) {
		if err := os.WriteFile(gitignorePath, []byte(defaultButtonsGitignore()), 0600); err != nil {
			return fmt.Errorf("failed to write .gitignore: %w", err)
		}
	} else {
		// Existing .gitignore — ensure the secret-bearing patterns are
		// present. Idempotent: only appends missing ones so a user who
		// customised their .gitignore keeps their additions.
		if err := ensureSecretPatterns(gitignorePath); err != nil {
			return fmt.Errorf("failed to update .gitignore: %w", err)
		}
	}
	return nil
}

// defaultButtonsGitignore is the template written on init. Lists
// every path in .buttons/ that can hold secrets or per-machine state
// and must never be committed.
//
// batteries.json — holds plaintext API keys. 0600 on disk, but that
//                  doesn't stop an accidental `git add -A`.
// webhook.json   — Cloudflare tunnel config (hostname + tunnel id).
//                  Not secret per se, but machine-specific.
// buttons/*/pressed/ — run history including stdin args; can leak
//                      sensitive values passed at press time.
// drawers/*/pressed/ — same, for drawer runs.
// idempotency/   — cached successful results keyed on args. Can
//                  contain secret-derived data.
// queues/        — file-lock semaphore state; machine-local.
func defaultButtonsGitignore() string {
	return `# Files that hold secrets — never commit.
batteries.json
# Machine-specific tunnel / listener config.
webhook.json
# Run history (per-machine, may contain sensitive args).
buttons/*/pressed/
drawers/*/pressed/
# Local execution state.
idempotency/
queues/
`
}

// secretPatterns is the set of .gitignore lines we consider
// load-bearing for secret hygiene. ensureSecretPatterns appends any
// missing ones to an existing .buttons/.gitignore so an older project
// upgrading past the init that introduced the pattern gets the
// coverage retroactively.
var secretPatterns = []string{
	"batteries.json",
	"webhook.json",
}

func ensureSecretPatterns(path string) error {
	data, err := os.ReadFile(path) // #nosec G304 -- path is .buttons/.gitignore
	if err != nil {
		return err
	}
	existing := string(data)
	var toAppend []string
	for _, pat := range secretPatterns {
		if !gitignoreContainsPattern(existing, pat) {
			toAppend = append(toAppend, pat)
		}
	}
	if len(toAppend) == 0 {
		return nil
	}
	// Preserve the user's trailing newline, then append our additions
	// with a header so it's clear they came from buttons upgrade.
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		existing += "\n"
	}
	existing += "\n# Added by buttons upgrade — never commit these.\n"
	for _, p := range toAppend {
		existing += p + "\n"
	}
	return os.WriteFile(path, []byte(existing), 0600)
}

// gitignoreContainsPattern scans a .gitignore body for a literal line
// matching `pattern`. Doesn't handle globs / negations — we only use
// it for our own fixed patterns so a simple line-equality check is
// fine and avoids pulling a .gitignore parser in.
func gitignoreContainsPattern(body, pattern string) bool {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == pattern {
			return true
		}
	}
	return false
}

// resolveAgentTargets picks the agent skill files to install based on
// (in priority order):
//
//  1. An explicit --agent flag (including "none" to disable).
//  2. Non-interactive modes (jsonOutput, noInput) → empty list.
//  3. Interactive TTY → run the multi-select picker.
//
// An unrecognized target ID in --agent surfaces as a clear error
// rather than silently skipping — less chance of "it didn't do what
// I asked and I didn't notice."
func resolveAgentTargets(cmd *cobra.Command) ([]string, error) {
	if cmd.Flags().Changed("agent") {
		// Explicit --agent none means "I thought about it, don't pick."
		if len(initAgents) == 1 && initAgents[0] == "none" {
			return nil, nil
		}
		for _, id := range initAgents {
			if _, ok := agentskill.TargetByID(id); !ok {
				return nil, fmt.Errorf("unknown agent: %q (valid: %s)", id, validAgentIDs())
			}
		}
		return initAgents, nil
	}

	if jsonOutput || noInput {
		return nil, nil
	}

	return pickAgentsInteractive()
}

// pickAgentsInteractive shows the Huh single-select picker. Users who
// need multiple agents can pass them via `--agent cursor,claude` — most
// people use one, and a single-select avoids the classic multi-select
// trap of hitting enter without first pressing space to toggle.
func pickAgentsInteractive() ([]string, error) {
	const skipID = "__none__"
	options := make([]huh.Option[string], 0, len(agentskill.Targets)+1)
	for _, t := range agentskill.Targets {
		options = append(options,
			huh.NewOption(fmt.Sprintf("%s  — %s", t.Label, t.Description), t.ID),
		)
	}
	options = append(options, huh.NewOption("None (skip)", skipID))

	var selected string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Install a Buttons skill file for your coding agent?").
				Description("Arrow keys to move, enter to confirm.").
				Options(options...).
				Value(&selected),
		),
	)
	if err := form.Run(); err != nil {
		// A user aborting the prompt (ctrl+c) is normal — treat as "no
		// selection" rather than a hard error that poisons the init.
		if err == huh.ErrUserAborted {
			return nil, nil
		}
		return nil, fmt.Errorf("agent picker: %w", err)
	}
	if selected == "" || selected == skipID {
		return nil, nil
	}
	return []string{selected}, nil
}

func validAgentIDs() string {
	ids := make([]string, 0, len(agentskill.Targets)+1)
	for _, t := range agentskill.Targets {
		ids = append(ids, t.ID)
	}
	ids = append(ids, "none")
	return fmt.Sprintf("%v", ids)
}

func printInitSummary(cwd string, alreadyExisted bool, agentMDPath string, installs []agentskill.WriteResult) error {
	if alreadyExisted {
		fmt.Fprintf(os.Stderr, ".buttons/ already exists in %s\n", cwd)
	} else {
		fmt.Fprintf(os.Stderr, "Initialized .buttons/ in %s\n", cwd)
	}
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "  Reference: %s\n", agentMDPath)

	for _, r := range installs {
		fmt.Fprintf(os.Stderr, "  %s %s: %s\n", r.Action, r.TargetID, r.Path)
	}

	if len(installs) == 0 {
		fmt.Fprintf(os.Stderr, "  No agent skill files installed. Re-run with --agent <list> to add one.\n")
	}

	fmt.Fprintf(os.Stderr, "\n")
	printNextHint("buttons create hello --code 'echo \"Hello from %s\"'", filepath.Base(cwd))
	return nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().StringSliceVar(
		&initAgents,
		"agent",
		nil,
		"agent integrations to install (cursor,claude,cline,copilot,agents-md); 'none' skips",
	)
}
