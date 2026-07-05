package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/config"
	"github.com/spf13/cobra"
)

var deleteForce bool

var deleteCmd = &cobra.Command{
	Use:     "delete [name]",
	Aliases: []string{"rm"},
	Short:   "Delete a button",
	Long: `Delete a button and all its history.

Prompts for confirmation unless --force is passed. In JSON or non-TTY
mode, confirmation is skipped automatically (agents are non-interactive).

Examples:
  buttons delete deploy
  buttons delete deploy -F
  buttons delete deploy --json`,
	Args:              exactArgs(1),
	ValidArgsFunction: completeFirstButtonName,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := button.Slugify(args[0])
		svc := button.NewService()

		if _, err := svc.Get(name); err != nil {
			return handleServiceError(err)
		}

		if !deleteForce {
			if jsonOutput || noInput || config.IsNonTTY() {
				deleteForce = true
			}
		}

		if !deleteForce {
			fmt.Fprintf(os.Stderr, "Delete button %q? [y/N] ", name)
			reader := bufio.NewReader(os.Stdin)
			line, _ := reader.ReadString('\n')
			if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "y") {
				fmt.Fprintln(os.Stderr, "Aborted.")
				return nil
			}
		}

		if err := svc.Remove(name); err != nil {
			return handleServiceError(err)
		}

		agentsMdAction := updateProjectAgentsMD(svc)

		if jsonOutput {
			out := map[string]any{"name": name, "deleted": true}
			if agentsMdAction != "" {
				out["agents_md"] = agentsMdAction
			}
			return config.WriteJSON(out)
		}

		fmt.Fprintf(os.Stderr, "Deleted button: %s\n", name)
		if agentsMdAction != "" {
			fmt.Fprintf(os.Stderr, "  AGENTS.md button list %s\n", agentsMdAction)
		}
		printNextHint("buttons list")
		return nil
	},
}

func init() {
	deleteCmd.Flags().BoolVarP(&deleteForce, "force", "F", false, "delete without confirmation")
	rootCmd.AddCommand(deleteCmd)
}
