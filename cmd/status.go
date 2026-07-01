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
	Short: "Show available CLI and button updates",
	Long: `Show whether the buttons CLI or installed registry buttons have updates.

Like every user-invoked command, status also enters the passive auto-update
gate before it prints. Use 'buttons update' to force an update immediately.`,
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
		fmt.Fprintln(os.Stderr, "Buttons: no registry-installed buttons found")
		return
	}
	available := 0
	for _, b := range report.Buttons {
		switch {
		case b.Skipped:
			fmt.Fprintf(os.Stderr, "Button %s: skipped (%s)\n", b.Name, b.SkipReason)
		case b.Error != "":
			fmt.Fprintf(os.Stderr, "Button %s: check failed: %s\n", b.Name, b.Error)
		case b.UpdateAvailable:
			available++
			fmt.Fprintf(os.Stderr, "Button %s: update available %s -> %s\n", b.Name, b.CurrentVersion, b.LatestVersion)
		}
	}
	if available == 0 {
		fmt.Fprintln(os.Stderr, "Buttons: up to date")
	}
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
