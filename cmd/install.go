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

Source resolution, in order: --source <dir> / $BUTTONS_SOURCE (a local source),
else $BUTTONS_REGISTRY_URL (the hosted registry, bearer-authed with the
REGISTRY_KEY battery or $BUTTONS_BAT_REGISTRY_KEY).

Examples:
  BUTTONS_REGISTRY_URL=https://registry.example buttons install @your-desk/hello
  buttons install deploy --source ../button-source
  buttons install deploy@1.2.0 --source ../button-source`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		src, sourceRef, err := resolveInstallSource()
		if err != nil {
			if jsonOutput {
				_ = config.WriteJSONError("VALIDATION_ERROR", err.Error())
				return errSilent
			}
			return err
		}

		res, err := store.InstallSpec(src, args[0], sourceRef)
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

// resolveInstallSource picks the install source: an explicit --source / $BUTTONS_SOURCE
// local directory wins; otherwise the hosted registry at $BUTTONS_REGISTRY_URL.
func resolveInstallSource() (store.Source, string, error) {
	dir := installSource
	if dir == "" {
		dir = os.Getenv("BUTTONS_SOURCE")
	}
	if dir != "" {
		return &store.LocalSource{Root: dir}, "local:" + dir, nil
	}
	if reg := strings.TrimRight(os.Getenv("BUTTONS_REGISTRY_URL"), "/"); reg != "" {
		key := registryKey()
		if key == "" {
			return nil, "", fmt.Errorf("registry key not set: run `buttons batteries set REGISTRY_KEY <key>` (or set $BUTTONS_BAT_REGISTRY_KEY)")
		}
		return &store.HTTPSource{BaseURL: reg, Key: key}, reg, nil
	}
	return nil, "", fmt.Errorf("no source: set $BUTTONS_REGISTRY_URL (+ REGISTRY_KEY battery) for the registry, or pass --source <dir> / $BUTTONS_SOURCE for a local source")
}

// registryKey resolves the registry bearer key — the BUTTONS_BAT_REGISTRY_KEY env
// (injected during a press) wins, else the REGISTRY_KEY battery.
func registryKey() string {
	if k := os.Getenv("BUTTONS_BAT_REGISTRY_KEY"); k != "" {
		return k
	}
	if svc, err := newBatteryService(); err == nil {
		if v, _, err := svc.Get("REGISTRY_KEY"); err == nil {
			return v
		}
	}
	return ""
}

func init() {
	installCmd.Flags().StringVar(&installSource, "source", "", "local source directory to install from (else $BUTTONS_REGISTRY_URL)")
	rootCmd.AddCommand(installCmd)
}
