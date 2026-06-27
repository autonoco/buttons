package cmd

import (
	"fmt"
	"os"

	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/store"
	"github.com/spf13/cobra"
)

var publishSource string

var publishCmd = &cobra.Command{
	Use:   "publish <name>",
	Short: "Publish a local button to a source so others can install it",
	Long: `Publish a button — the inverse of 'buttons install'. The button folder
(button.json + code + AGENT.md, never its run history) is content-hashed and
written to a source, where 'buttons install <name> --source <dir>' can fetch it.

The registry source (buttons.co, #275/#276) is not built yet; for now publish
to a local source directory with --source (or $BUTTONS_SOURCE). That directory
is a valid install source, so publish + install round-trip end-to-end today.

Examples:
  buttons publish deploy --source ./pack
  BUTTONS_SOURCE=./pack buttons publish deploy`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		srcDir := publishSource
		if srcDir == "" {
			srcDir = os.Getenv("BUTTONS_SOURCE")
		}
		if srcDir == "" {
			msg := "no source: pass --source <dir> or set $BUTTONS_SOURCE (the registry publish target lands in #276)"
			if jsonOutput {
				_ = config.WriteJSONError("VALIDATION_ERROR", msg)
				return errSilent
			}
			return fmt.Errorf("%s", msg)
		}

		dst := &store.LocalSource{Root: srcDir}
		res, err := store.Publish(dst, args[0])
		if err != nil {
			if jsonOutput {
				_ = config.WriteJSONError("PUBLISH_ERROR", err.Error())
				return errSilent
			}
			return err
		}

		if jsonOutput {
			return config.WriteJSON(res)
		}
		fmt.Fprintf(os.Stderr, "Published %s (%d files, sha256 %s) to %s\n", res.Name, res.Files, res.SHA256[:12], srcDir)
		printNextHint("buttons install %s --source %s", res.Name, srcDir)
		return nil
	},
}

func init() {
	publishCmd.Flags().StringVar(&publishSource, "source", "", "source directory to publish to (until the registry lands, #276)")
	rootCmd.AddCommand(publishCmd)
}
