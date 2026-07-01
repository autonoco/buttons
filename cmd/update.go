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
	Short: "Update the CLI and floating button dependencies",
	Long: `Install available updates for the buttons CLI and floating button dependencies.

The CLI binary is updated from GitHub Releases. Button dependencies are
refreshed from .buttons/buttons.json and .buttons/buttons-lock.json. Exact
versions are pins; update moves only dependencies requested as "latest".

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
		fmt.Fprintln(os.Stderr, "No manifest dependencies found.")
		return
	}
	updated := 0
	for _, b := range result.Buttons {
		switch {
		case b.Updated:
			updated++
			fmt.Fprintf(os.Stderr, "Updated %s to %s\n", b.Name, b.LatestVersion)
		case b.Pinned && b.CurrentVersion == "":
			fmt.Fprintf(os.Stderr, "Button %s pinned at %s (not installed; run buttons install)\n", b.PackageName, b.Requested)
		case b.Pinned && b.LatestVersion != "" && b.CurrentVersion != "" && b.LatestVersion != b.CurrentVersion:
			fmt.Fprintf(os.Stderr, "Button %s pinned at %s (latest %s)\n", b.Name, b.CurrentVersion, b.LatestVersion)
		case b.Pinned:
			fmt.Fprintf(os.Stderr, "Button %s pinned at %s\n", b.Name, b.CurrentVersion)
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
