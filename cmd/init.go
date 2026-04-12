package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/autonoco/buttons/internal/config"
	"github.com/spf13/cobra"
)

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

Examples:
  cd my-project
  buttons init
  buttons create deploy --code './scripts/deploy.sh' --arg env:string:required
  # → button lives in my-project/.buttons/buttons/deploy/`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("cannot determine working directory: %w", err)
		}

		buttonsDir := filepath.Join(cwd, ".buttons")

		if info, err := os.Stat(buttonsDir); err == nil && info.IsDir() {
			if jsonOutput {
				return config.WriteJSON(map[string]any{
					"path":    buttonsDir,
					"created": false,
					"message": ".buttons already exists",
				})
			}
			fmt.Fprintf(os.Stderr, ".buttons/ already exists in %s\n", cwd)
			return nil
		}

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

		// Create a .gitignore inside .buttons/ that excludes run history
		// but keeps button specs committed.
		gitignore := `# Button specs, code files, and AGENT.md are committed.
# Run history is per-machine and excluded.
buttons/*/pressed/
`
		gitignorePath := filepath.Join(buttonsDir, ".gitignore")
		if err := os.WriteFile(gitignorePath, []byte(gitignore), 0600); err != nil {
			return fmt.Errorf("failed to write .gitignore: %w", err)
		}

		if jsonOutput {
			return config.WriteJSON(map[string]any{
				"path":    buttonsDir,
				"created": true,
			})
		}

		fmt.Fprintf(os.Stderr, "Initialized .buttons/ in %s\n", cwd)
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "  Buttons created here are project-local and can be committed to git.\n")
		fmt.Fprintf(os.Stderr, "  Run history (pressed/) is gitignored automatically.\n")
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "  Try: buttons create hello --code 'echo \"Hello from %s\"'\n", filepath.Base(cwd))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}
