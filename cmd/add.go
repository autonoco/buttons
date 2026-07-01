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

var addCmd = &cobra.Command{
	Use:   "add @desk/name[@version]",
	Short: "Add a button dependency",
	Long: `Add a registry button dependency to .buttons/buttons.json and install it.

Bare package names are not supported in the MVP. Use scoped names like
@autono/hello. Omit @version to track latest; include @version to pin
an exact immutable version.

Examples:
  buttons add @your-desk/hello
  buttons add @your-desk/hello@1`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		res, name, requested, err := runAdd(context.Background(), args[0])
		if err != nil {
			if jsonOutput {
				_ = config.WriteJSONError("ADD_ERROR", err.Error())
				return errSilent
			}
			return err
		}
		if jsonOutput {
			return config.WriteJSON(map[string]any{
				"name":      name,
				"requested": requested,
				"installed": res.Installed,
			})
		}
		fmt.Fprintf(os.Stderr, "Added %s@%s\n", name, requested)
		if len(res.Installed) > 0 {
			fmt.Fprintf(os.Stderr, "Installed %d button(s): %s\n", len(res.Installed), strings.Join(res.Installed, ", "))
		}
		printNextHint("buttons status")
		return nil
	},
}

func runAdd(ctx context.Context, spec string) (*store.Result, string, string, error) {
	_ = ctx
	name, requested, err := manifest.ParsePackageSpec(spec)
	if err != nil {
		return nil, "", "", err
	}
	m, err := manifest.Load()
	if err != nil {
		if os.IsNotExist(err) {
			m = manifest.New()
		} else {
			return nil, "", "", err
		}
	}
	if m.Dependencies == nil {
		m.Dependencies = map[string]string{}
	}
	m.Dependencies[name] = requested
	if err := m.Validate(); err != nil {
		return nil, "", "", err
	}
	src, err := resolveRegistrySource()
	if err != nil {
		return nil, "", "", err
	}
	lock, err := manifest.LoadLockfile()
	if err != nil {
		return nil, "", "", err
	}
	res, next, err := store.InstallManifest(src, m, lock, store.InstallOptions{RefreshFloating: true})
	if err != nil {
		return nil, "", "", err
	}
	if err := manifest.Save(m); err != nil {
		return nil, "", "", err
	}
	if err := manifest.SaveLockfile(next); err != nil {
		return nil, "", "", err
	}
	recordLifecycleHistory("add", name, requested, res.Installed, next)
	return res, name, requested, nil
}

func init() {
	rootCmd.AddCommand(addCmd)
}
