package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/history"
	"github.com/spf13/cobra"
)

var historyLast int

var historyCmd = &cobra.Command{
	Use:   "history [button-name]",
	Short: "Show run history",
	Long: `Show execution history for buttons.

Displays recent presses with status, exit code, duration, and timestamp.
Optionally filter by button name. Results are ordered most recent first.

Examples:
  buttons history
  buttons history deploy
  buttons history deploy --last 5
  buttons history --json`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var runs []history.Run
		var err error

		if len(args) > 0 {
			runs, err = history.List(args[0], historyLast)
		} else {
			runs, err = history.ListAll(historyLast)
		}
		if err != nil {
			if jsonOutput {
				_ = config.WriteJSONError("STORAGE_ERROR", err.Error())
				return errSilent
			}
			return fmt.Errorf("failed to list history: %w", err)
		}

		if jsonOutput {
			return config.WriteJSON(runs)
		}

		if len(runs) == 0 {
			fmt.Fprintln(os.Stderr, "No runs found.")
			return nil
		}

		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "BUTTON\tSTATUS\tEXIT\tDURATION\tSTARTED")
		for _, r := range runs {
			fmt.Fprintf(w, "%s\t%s\t%d\t%dms\t%s\n",
				r.ButtonName,
				r.Status,
				r.ExitCode,
				r.DurationMs,
				r.StartedAt.Format("2006-01-02 15:04:05"),
			)
		}
		return w.Flush()
	},
}

func init() {
	historyCmd.Flags().IntVar(&historyLast, "last", 20, "number of runs to show")
	rootCmd.AddCommand(historyCmd)
}
