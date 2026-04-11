package cmd

import (
	"fmt"
	"os"

	"github.com/autonoco/buttons/internal/config"
	"github.com/spf13/cobra"
)

var batteriesCmd = &cobra.Command{
	Use:   "batteries",
	Short: "Manage environment variables and secrets",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var batteriesSetCmd = &cobra.Command{
	Use:   "set [key] [value]",
	Short: "Set an environment variable",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO: implement in Phase 2.2 (#264)
		if jsonOutput {
			_ = config.WriteJSONError("NOT_IMPLEMENTED", "batteries set not yet implemented")
			return errSilent
		}
		fmt.Fprintln(os.Stderr, "batteries set: not yet implemented")
		return nil
	},
}

var batteriesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all environment variables",
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO: implement in Phase 2.2 (#264)
		if jsonOutput {
			_ = config.WriteJSONError("NOT_IMPLEMENTED", "batteries list not yet implemented")
			return errSilent
		}
		fmt.Fprintln(os.Stderr, "batteries list: not yet implemented")
		return nil
	},
}

var batteriesRmCmd = &cobra.Command{
	Use:   "rm [key]",
	Short: "Remove an environment variable",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO: implement in Phase 2.2 (#264)
		if jsonOutput {
			_ = config.WriteJSONError("NOT_IMPLEMENTED", "batteries rm not yet implemented")
			return errSilent
		}
		fmt.Fprintln(os.Stderr, "batteries rm: not yet implemented")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(batteriesCmd)
	batteriesCmd.AddCommand(batteriesSetCmd)
	batteriesCmd.AddCommand(batteriesListCmd)
	batteriesCmd.AddCommand(batteriesRmCmd)
}
