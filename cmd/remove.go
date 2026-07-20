package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/manifest"
	"github.com/autonoco/buttons/internal/store"
	"github.com/spf13/cobra"
)

var removeCmd = &cobra.Command{
	Use:   "remove @desk/name",
	Short: "Remove a registry package dependency",
	Long: `Remove a registry package dependency added with 'buttons add'.

Remove drops the dependency from .buttons/buttons.json, deletes the
installed package directory, and cleans .buttons/buttons-lock.json. Only
directories the installer materialized (stamped with install state) are
deleted; a package another installed package still requires keeps its
files until its last dependent is removed too.

Use 'buttons delete' to delete a locally created button.

Examples:
  buttons remove @your-desk/hello
  buttons remove @your-desk/hello --json`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeDependencyNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		res, name, err := runRemove(context.Background(), args[0])
		if err != nil {
			if jsonOutput {
				_ = config.WriteJSONError("REMOVE_ERROR", err.Error())
				return errSilent
			}
			return err
		}
		agentsMdAction := updateProjectAgentsMD(button.NewService())

		if jsonOutput {
			out := map[string]any{
				"name":    name,
				"removed": res.Removed,
			}
			if len(res.Kept) > 0 {
				out["kept"] = res.Kept
			}
			if agentsMdAction != "" {
				out["agents_md"] = agentsMdAction
			}
			return config.WriteJSON(out)
		}
		fmt.Fprintf(os.Stderr, "Removed %s\n", name)
		if len(res.Removed) > 0 {
			fmt.Fprintf(os.Stderr, "Deleted %d package item(s): %s\n", len(res.Removed), strings.Join(res.Removed, ", "))
		}
		if len(res.Kept) > 0 {
			fmt.Fprintf(os.Stderr, "Kept %d package item(s) still in use: %s\n", len(res.Kept), strings.Join(res.Kept, ", "))
		}
		if agentsMdAction != "" {
			fmt.Fprintf(os.Stderr, "  AGENTS.md button list %s\n", agentsMdAction)
		}
		printNextHint("buttons list")
		return nil
	},
}

func runRemove(ctx context.Context, spec string) (*store.UninstallResult, string, error) {
	_ = ctx
	if !strings.HasPrefix(spec, "@") {
		return nil, "", fmt.Errorf("%q is not a scoped package name: registry dependencies look like @desk/name; use `buttons delete %s` for a local button", spec, spec)
	}
	name, _, err := manifest.ParsePackageSpec(spec)
	if err != nil {
		return nil, "", err
	}
	m, err := manifest.Load()
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", fmt.Errorf("no .buttons/buttons.json found: nothing to remove")
		}
		return nil, "", err
	}
	requested, ok := m.Dependencies[name]
	if !ok {
		return nil, "", fmt.Errorf("%s is not a dependency in .buttons/buttons.json", name)
	}
	delete(m.Dependencies, name)
	lock, err := manifest.LoadLockfile()
	if err != nil {
		return nil, "", err
	}
	res, err := store.Uninstall(lock, name)
	if err != nil {
		return nil, "", err
	}
	if err := manifest.Save(m); err != nil {
		return nil, "", err
	}
	if err := manifest.SaveLockfile(lock); err != nil {
		return nil, "", err
	}
	recordLifecycleHistory("remove", name, requested, res.Removed, lock)
	return res, name, nil
}

// completeDependencyNames completes the scoped package names declared in
// .buttons/buttons.json — the only valid arguments for `buttons remove`.
func completeDependencyNames(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	m, err := manifest.Load()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	names := make([]string, 0, len(m.Dependencies))
	for name := range m.Dependencies {
		if strings.HasPrefix(name, toComplete) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, cobra.ShellCompDirectiveNoFileComp
}

func init() {
	rootCmd.AddCommand(removeCmd)
}
