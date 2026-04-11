package cmd

import (
	"fmt"
	"os"

	"github.com/autonoco/buttons/internal/config"
	"github.com/spf13/cobra"
)

var smashCmd = &cobra.Command{
	Use:   "smash [buttons...]",
	Short: "Run multiple buttons in parallel",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO: implement in Phase 3.1 (#267)
		if jsonOutput {
			_ = config.WriteJSONError("NOT_IMPLEMENTED", "smash not yet implemented")
			return errSilent
		}
		fmt.Fprintln(os.Stderr, "smash: not yet implemented")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(smashCmd)
}
