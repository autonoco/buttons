package cmd

import (
	"fmt"
	"os"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/tui"
	"github.com/spf13/cobra"
)

var boardCmd = &cobra.Command{
	Use:   "board [name]",
	Short: "Show the button board (TUI dashboard)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// JSON mode doesn't make sense for a TUI — bail early with the
		// same not-implemented shape the other structured outputs use.
		if jsonOutput {
			_ = config.WriteJSONError("NOT_APPLICABLE", "board is an interactive TUI; --json is not supported")
			return errSilent
		}

		var initial string
		if len(args) > 0 {
			initial = args[0]
		}

		svc := button.NewService()
		if err := tui.Run(svc, initial); err != nil {
			fmt.Fprintf(os.Stderr, "board: %v\n", err)
			return errSilent
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(boardCmd)
}
