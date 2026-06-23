package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/store"
	"github.com/spf13/cobra"
)

var installSource string

var installCmd = &cobra.Command{
	Use:   "install <name | tag:x>",
	Short: "Install a button (or every button with a tag) from a source",
	Long: `Install buttons from a source into your buttons directory.

The argument is one of:
  <name>            a single button (latest version)
  <name>@<version>  a pinned version
  tag:<tag>         every button in the source carrying <tag>

Each installed button's dependencies (its button.json "requires") are
installed too. Source + version + content hash are recorded in each
installed button.json for pinning and updates.

The registry source (buttons.co, #275) is not built yet; for now pass a
local source directory with --source (or $BUTTONS_SOURCE).

Examples:
  buttons install deploy --source ./pack
  buttons install tag:autono-cal --source ./pack
  buttons install deploy@1.2.0 --source ./pack`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		srcDir := installSource
		if srcDir == "" {
			srcDir = os.Getenv("BUTTONS_SOURCE")
		}
		if srcDir == "" {
			msg := "no source: pass --source <dir> or set $BUTTONS_SOURCE (the registry source lands in #275)"
			if jsonOutput {
				_ = config.WriteJSONError("VALIDATION_ERROR", msg)
				return errSilent
			}
			return fmt.Errorf("%s", msg)
		}

		src := &store.LocalSource{Root: srcDir}
		res, err := store.InstallSpec(src, args[0], "local:"+srcDir)
		if err != nil {
			if jsonOutput {
				_ = config.WriteJSONError("INSTALL_ERROR", err.Error())
				return errSilent
			}
			return err
		}

		if jsonOutput {
			return config.WriteJSON(res)
		}
		fmt.Fprintf(os.Stderr, "Installed %d button(s): %s\n", len(res.Installed), strings.Join(res.Installed, ", "))
		printNextHint("buttons list")
		return nil
	},
}

func init() {
	installCmd.Flags().StringVar(&installSource, "source", "", "source directory to install from (until the registry lands, #275)")
	rootCmd.AddCommand(installCmd)
}
