package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/updater"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show available CLI and package updates",
	Long: `Show whether the buttons CLI or manifest dependencies have updates.

Like every user-invoked command, status also enters enabled passive update
paths before it prints. buttons-auto-update may refresh floating package
dependencies; cli-auto-update may update the CLI binary when the throttle
allows. Use 'buttons update' to force the full update path immediately.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		report, err := updater.Check(context.Background(), updater.Options{
			CurrentVersion: version,
			RegistryURL:    registryURL(),
			RegistryKey:    registryKey(),
		})
		if err != nil {
			if jsonOutput {
				_ = config.WriteJSONError("STATUS_ERROR", err.Error())
				return errSilent
			}
			return err
		}
		if jsonOutput {
			return config.WriteJSON(report)
		}
		printStatus(report)
		return nil
	},
}

func printStatus(report *updater.Report) {
	if report.Binary != nil {
		switch {
		case report.Binary.Error != "":
			fmt.Fprintf(os.Stderr, "CLI: check failed: %s\n", report.Binary.Error)
		case report.Binary.UpdateAvailable:
			fmt.Fprintf(os.Stderr, "CLI: update available %s -> %s\n", report.Binary.Current, report.Binary.Latest)
		default:
			fmt.Fprintf(os.Stderr, "CLI: up to date (%s)\n", report.Binary.Current)
		}
	}

	if len(report.Buttons) == 0 {
		fmt.Fprintln(os.Stderr, "Buttons: no manifest dependencies found")
		return
	}
	available := 0
	for _, b := range report.Buttons {
		label := packageKindLabel(b.Kind)
		switch {
		case b.Pinned && b.CurrentVersion == "":
			fmt.Fprintf(os.Stderr, "%s %s: pinned at %s (not installed; run buttons install)\n", label, b.PackageName, b.Requested)
		case b.Pinned && b.LatestVersion != "" && b.CurrentVersion != "" && b.LatestVersion != b.CurrentVersion:
			fmt.Fprintf(os.Stderr, "%s %s: pinned at %s (latest %s)\n", label, b.Name, b.CurrentVersion, b.LatestVersion)
		case b.Pinned:
			fmt.Fprintf(os.Stderr, "%s %s: pinned at %s\n", label, b.Name, b.CurrentVersion)
		case b.Skipped:
			fmt.Fprintf(os.Stderr, "%s %s: skipped (%s)\n", label, b.Name, b.SkipReason)
		case b.Error != "":
			fmt.Fprintf(os.Stderr, "%s %s: check failed: %s\n", label, b.Name, b.Error)
		case b.UpdateAvailable:
			available++
			fmt.Fprintf(os.Stderr, "%s %s: update available %s -> %s\n", label, b.Name, b.CurrentVersion, b.LatestVersion)
		}
	}
	if available == 0 {
		fmt.Fprintln(os.Stderr, "Buttons: up to date")
	}
}

func packageKindLabel(kind string) string {
	if kind == "drawer" {
		return "Drawer"
	}
	return "Button"
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
