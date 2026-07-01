package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/updater"
	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update the CLI and installed registry buttons",
	Long: `Install available updates for the buttons CLI and installed registry buttons.

The CLI binary is updated from GitHub Releases. Installed buttons are refreshed
from the source stamped in each installed button.json.

Examples:
  buttons update
  buttons update --json`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		result, err := updater.Apply(context.Background(), updater.Options{
			CurrentVersion: version,
			RegistryURL:    registryURL(),
			RegistryKey:    registryKey(),
			Writer:         os.Stderr,
		})
		if err != nil {
			if jsonOutput {
				_ = config.WriteJSONError("UPDATE_ERROR", err.Error())
				return errSilent
			}
			return err
		}
		if jsonOutput {
			return config.WriteJSON(result)
		}
		printUpdateResult(result)
		return nil
	},
}

func printUpdateResult(result *updater.Result) {
	if result.Binary != nil {
		switch {
		case result.UpdatedBinary:
			fmt.Fprintf(os.Stderr, "Updated CLI to %s\n", result.Binary.Latest)
		case result.Binary.Error != "":
			fmt.Fprintf(os.Stderr, "CLI update skipped: %s\n", result.Binary.Error)
		case result.Binary.UpdateAvailable:
			fmt.Fprintf(os.Stderr, "CLI update available: %s -> %s\n", result.Binary.Current, result.Binary.Latest)
		default:
			fmt.Fprintf(os.Stderr, "CLI already up to date (%s)\n", result.Binary.Current)
		}
	}

	if len(result.Buttons) == 0 {
		fmt.Fprintln(os.Stderr, "No registry-installed buttons found.")
		return
	}
	updated := 0
	for _, b := range result.Buttons {
		switch {
		case b.Updated:
			updated++
			fmt.Fprintf(os.Stderr, "Updated %s to %s\n", b.Name, b.LatestVersion)
		case b.Skipped:
			fmt.Fprintf(os.Stderr, "Button %s skipped: %s\n", b.Name, b.SkipReason)
		case b.Error != "":
			fmt.Fprintf(os.Stderr, "Button %s skipped: %s\n", b.Name, b.Error)
		case b.UpdateAvailable:
			fmt.Fprintf(os.Stderr, "Button update available: %s %s -> %s\n", b.Name, b.CurrentVersion, b.LatestVersion)
		}
	}
	if updated == 0 {
		fmt.Fprintln(os.Stderr, "Installed buttons already up to date.")
	}
}

func init() {
	rootCmd.AddCommand(updateCmd)
}
