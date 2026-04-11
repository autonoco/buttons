package cmd

import (
	"fmt"
	"os"

	"github.com/autonoco/buttons/internal/config"
	"github.com/spf13/cobra"
)

var boardCmd = &cobra.Command{
	Use:   "board [name]",
	Short: "Show the button board (TUI dashboard)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO: implement in Phase 3.2 (#268)
		if jsonOutput {
			_ = config.WriteJSONError("NOT_IMPLEMENTED", "board not yet implemented")
			return errSilent
		}
		fmt.Fprintln(os.Stderr, "board: not yet implemented")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(boardCmd)
}
