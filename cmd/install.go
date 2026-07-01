package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/manifest"
	"github.com/autonoco/buttons/internal/store"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install buttons from .buttons/buttons.json",
	Long: `Install buttons declared in .buttons/buttons.json.

Use 'buttons add @desk/name' to add a dependency. Use 'buttons install'
to materialize the dependency manifest into .buttons/buttons/ and refresh
.buttons/buttons-lock.json.

Examples:
  buttons install
  buttons add @your-desk/hello
  buttons add @your-desk/hello@1`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return nil
		}
		return fmt.Errorf("use `buttons add %s` to add a dependency, or `buttons install` to install from .buttons/buttons.json", args[0])
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		res, err := runManifestInstall(context.Background(), false)
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

func runManifestInstall(ctx context.Context, refreshFloating bool) (*store.Result, error) {
	_ = ctx // reserved for future cancellable registry fetches
	m, err := manifest.Load()
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no .buttons/buttons.json found: run `buttons add @desk/name` first")
		}
		return nil, err
	}
	src, err := resolveRegistrySource()
	if err != nil {
		return nil, err
	}
	lock, err := manifest.LoadLockfile()
	if err != nil {
		return nil, err
	}
	res, next, err := store.InstallManifest(src, m, lock, store.InstallOptions{RefreshFloating: refreshFloating})
	if err != nil {
		return nil, err
	}
	if err := manifest.SaveLockfile(next); err != nil {
		return nil, err
	}
	recordLifecycleHistory("install", "", "", res.Installed, next)
	return res, nil
}

func resolveRegistrySource() (store.Source, error) {
	reg := registryURL()
	if reg == "" {
		return nil, fmt.Errorf("registry URL not set: set $BUTTONS_REGISTRY_URL")
	}
	key := registryKey()
	if key == "" {
		return nil, fmt.Errorf("registry key not set: run `buttons batteries set REGISTRY_KEY <key>` (or set $BUTTONS_BAT_REGISTRY_KEY)")
	}
	return &store.HTTPSource{BaseURL: reg, Key: key}, nil
}

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

func registryURL() string {
	return strings.TrimRight(os.Getenv("BUTTONS_REGISTRY_URL"), "/")
}

func init() {
	rootCmd.AddCommand(installCmd)
}
