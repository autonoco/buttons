package cmd

import (
	"fmt"
	"os"

	"github.com/autonoco/buttons/internal/config"
	"github.com/spf13/cobra"
)

var storeCmd = &cobra.Command{
	Use:   "store",
	Short: "Marketplace (search/install/import/publish)",
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO: implement in Phase 5.1 (#274)
		if jsonOutput {
			_ = config.WriteJSONError("NOT_IMPLEMENTED", "store not yet implemented")
			return errSilent
		}
		fmt.Fprintln(os.Stderr, "store: not yet implemented")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(storeCmd)
}
