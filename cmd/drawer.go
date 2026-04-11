package cmd

import (
	"fmt"
	"os"

	"github.com/autonoco/buttons/internal/config"
	"github.com/spf13/cobra"
)

var drawerCmd = &cobra.Command{
	Use:   "drawer",
	Short: "Manage button groups (drawers)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var drawerCreateCmd = &cobra.Command{
	Use:   "create [name]",
	Short: "Create a new drawer",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO: implement in Phase 2.1 (#263)
		if jsonOutput {
			_ = config.WriteJSONError("NOT_IMPLEMENTED", "drawer create not yet implemented")
			return errSilent
		}
		fmt.Fprintln(os.Stderr, "drawer create: not yet implemented")
		return nil
	},
}

var drawerAddCmd = &cobra.Command{
	Use:   "add [drawer] [button]",
	Short: "Add a button to a drawer",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO: implement in Phase 2.1 (#263)
		if jsonOutput {
			_ = config.WriteJSONError("NOT_IMPLEMENTED", "drawer add not yet implemented")
			return errSilent
		}
		fmt.Fprintln(os.Stderr, "drawer add: not yet implemented")
		return nil
	},
}

var drawerListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all drawers",
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO: implement in Phase 2.1 (#263)
		if jsonOutput {
			_ = config.WriteJSONError("NOT_IMPLEMENTED", "drawer list not yet implemented")
			return errSilent
		}
		fmt.Fprintln(os.Stderr, "drawer list: not yet implemented")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(drawerCmd)
	drawerCmd.AddCommand(drawerCreateCmd)
	drawerCmd.AddCommand(drawerAddCmd)
	drawerCmd.AddCommand(drawerListCmd)
}
