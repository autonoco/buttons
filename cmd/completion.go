package cmd

import (
	"strings"

	"github.com/autonoco/buttons/internal/button"
	"github.com/spf13/cobra"
)

func completeFirstButtonName(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return completeButtonNames(cmd, args, toComplete)
}

func completeButtonNames(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	used := make(map[string]struct{}, len(args))
	for _, arg := range args {
		used[button.Slugify(arg)] = struct{}{}
	}

	buttons, err := button.NewService().List()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	completions := make([]string, 0, len(buttons))
	for _, btn := range buttons {
		if _, ok := used[btn.Name]; ok {
			continue
		}
		if strings.HasPrefix(btn.Name, toComplete) {
			completions = append(completions, btn.Name)
		}
	}
	return completions, cobra.ShellCompDirectiveNoFileComp
}
