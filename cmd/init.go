package cmd

import (
	"fmt"
	"os"
	"path/filepath"

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
		gitignore := "# Button specs, code files, and AGENT.md are committed.\n" +
			"# Run history is per-machine and excluded.\n" +
			"buttons/*/pressed/\n"
		if err := os.WriteFile(gitignorePath, []byte(gitignore), 0600); err != nil {
			return fmt.Errorf("failed to write .gitignore: %w", err)
		}
	}
	return nil
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

// pickAgentsInteractive shows the Huh multi-select picker. Returns the
// set of selected target IDs, or an empty slice if the user submits
// with nothing picked (implicit "none").
func pickAgentsInteractive() ([]string, error) {
	options := make([]huh.Option[string], 0, len(agentskill.Targets))
	for _, t := range agentskill.Targets {
		options = append(options,
			huh.NewOption(fmt.Sprintf("%s  — %s", t.Label, t.Description), t.ID),
		)
	}

	var selected []string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Install a Buttons skill file for your coding agent?").
				Description("Space toggles, enter confirms. Select none to skip.").
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
	return selected, nil
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
	fmt.Fprintf(os.Stderr, "  Try: buttons create hello --code 'echo \"Hello from %s\"'\n", filepath.Base(cwd))
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
