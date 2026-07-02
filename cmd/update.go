package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/settings"
	"github.com/autonoco/buttons/internal/updater"
	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update the CLI and floating package dependencies",
	Long: `Install available updates for the buttons CLI and floating package dependencies.

The CLI binary is updated from GitHub Releases. Package dependencies are
refreshed from .buttons/buttons.json and .buttons/buttons-lock.json. Exact
versions are pins; update moves only dependencies requested as "latest".

Homebrew-managed installs are left to Homebrew by default. To let Buttons update
the CLI through Homebrew and run passive CLI binary updates when the throttle
allows, run:

  buttons config set cli-auto-update true

Examples:
  buttons update
  buttons update --json`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		result, err := updater.Apply(context.Background(), updaterOptions(os.Stderr))
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

func updaterOptions(writer io.Writer) updater.Options {
	opts := updater.Options{
		CurrentVersion: version,
		RegistryURL:    registryURL(),
		RegistryKey:    registryKey(),
		Writer:         writer,
	}
	if svc, err := settings.NewServiceFromEnv(); err == nil {
		if st, err := svc.Load(); err == nil {
			opts.CLIAutoUpdate = st.CLIAutoUpdateEnabled()
		}
	}
	return opts
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
		label := packageKindLabel(b.Kind)
		switch {
		case b.Updated:
			updated++
			fmt.Fprintf(os.Stderr, "Updated %s %s to %s\n", strings.ToLower(label), b.Name, b.LatestVersion)
		case b.Pinned && b.CurrentVersion == "":
			fmt.Fprintf(os.Stderr, "%s %s pinned at %s (not installed; run buttons install)\n", label, b.PackageName, b.Requested)
		case b.Pinned && b.LatestVersion != "" && b.CurrentVersion != "" && b.LatestVersion != b.CurrentVersion:
			fmt.Fprintf(os.Stderr, "%s %s pinned at %s (latest %s)\n", label, b.Name, b.CurrentVersion, b.LatestVersion)
		case b.Pinned:
			fmt.Fprintf(os.Stderr, "%s %s pinned at %s\n", label, b.Name, b.CurrentVersion)
		case b.Skipped:
			fmt.Fprintf(os.Stderr, "%s %s skipped: %s\n", label, b.Name, b.SkipReason)
		case b.Error != "":
			fmt.Fprintf(os.Stderr, "%s %s skipped: %s\n", label, b.Name, b.Error)
		case b.UpdateAvailable:
			fmt.Fprintf(os.Stderr, "%s update available: %s %s -> %s\n", label, b.Name, b.CurrentVersion, b.LatestVersion)
		}
	}
	if updated == 0 {
		fmt.Fprintln(os.Stderr, "Installed packages already up to date.")
	}
}

func init() {
	rootCmd.AddCommand(updateCmd)
}
