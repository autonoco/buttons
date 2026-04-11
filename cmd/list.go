package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/config"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all buttons",
	Long: `List all buttons in the registry.

Displays a table of buttons with name, runtime, file path, and timeout.
In non-TTY or --json mode, outputs the full button specs as JSON.

Examples:
  buttons list
  buttons list --json`,
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		svc := button.NewService()
		buttons, err := svc.List()
		if err != nil {
			return handleServiceError(err)
		}

		if jsonOutput {
			return config.WriteJSON(buttons)
		}

		if len(buttons) == 0 {
			fmt.Fprintln(os.Stderr, "No buttons found.")
			return nil
		}

		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tDESCRIPTION\tRUNTIME\tTIMEOUT")
		for _, btn := range buttons {
			desc := btn.Description
			if desc == "" {
				desc = "-"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%ds\n", btn.Name, desc, btn.Runtime, btn.TimeoutSeconds)
		}
		return w.Flush()
	},
}

func init() {
	rootCmd.AddCommand(listCmd)
}
