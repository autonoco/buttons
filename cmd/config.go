package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/settings"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Read and write per-user settings",
	Long: `Manage per-user defaults stored in ~/.buttons/settings.json.

Settings are global only — personal preferences shouldn't leak into
project repos via .buttons/. Project-level knobs live on each button
(e.g. 'buttons create --timeout N' pins a timeout for that button
specifically).

Known keys:
  default-timeout   seconds used when 'buttons create' is run without
                    an explicit --timeout flag (fallback: 300)
  theme             board TUI theme: default | paper | phosphor | amber
                    (fallback: default — adapts to terminal background)

Running ` + "`buttons config`" + ` with no subcommand prints the current values.

Resolution order for theme at TUI startup: $BUTTONS_THEME env var wins,
then settings, then default. Env override keeps A/B comparison easy.

Examples:
  buttons config
  buttons config set default-timeout 600
  buttons config set theme amber
  buttons config unset theme`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return showConfig()
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set KEY VALUE",
	Short: "Set a setting",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := settings.NewServiceFromEnv()
		if err != nil {
			return handleSettingsError(err)
		}
		if err := svc.Set(args[0], args[1]); err != nil {
			return handleSettingsError(err)
		}
		if jsonOutput {
			return config.WriteJSON(map[string]any{"key": args[0], "value": args[1]})
		}
		fmt.Fprintf(os.Stderr, "set %s = %s\n", args[0], args[1])
		return nil
	},
}

var configUnsetCmd = &cobra.Command{
	Use:   "unset KEY",
	Short: "Clear a setting (revert to built-in default)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := settings.NewServiceFromEnv()
		if err != nil {
			return handleSettingsError(err)
		}
		if err := svc.Unset(args[0]); err != nil {
			return handleSettingsError(err)
		}
		if jsonOutput {
			return config.WriteJSON(map[string]any{"key": args[0], "unset": true})
		}
		fmt.Fprintf(os.Stderr, "unset %s\n", args[0])
		return nil
	},
}

// showConfig prints every known setting with its current value and
// fallback source — so the user can see what's set vs what's using
// the built-in default without hunting through the help output.
func showConfig() error {
	svc, err := settings.NewServiceFromEnv()
	if err != nil {
		return handleSettingsError(err)
	}
	st, err := svc.Load()
	if err != nil {
		return handleSettingsError(err)
	}

	if jsonOutput {
		payload := map[string]any{}
		if v, ok := st.DefaultTimeout(); ok {
			payload[settings.KeyDefaultTimeout] = v
		}
		if v, ok := st.Theme(); ok {
			payload[settings.KeyTheme] = v
		}
		return config.WriteJSON(payload)
	}

	render := func(key string, value any, fallback string) {
		if value == nil {
			fmt.Fprintf(os.Stderr, "  %-18s  (unset — using %s)\n", key, fallback)
			return
		}
		fmt.Fprintf(os.Stderr, "  %-18s  %v\n", key, value)
	}
	if v, ok := st.DefaultTimeout(); ok {
		render(settings.KeyDefaultTimeout, v, "")
	} else {
		render(settings.KeyDefaultTimeout, nil, "300s")
	}
	if v, ok := st.Theme(); ok {
		render(settings.KeyTheme, v, "")
	} else {
		render(settings.KeyTheme, nil, "default (adaptive)")
	}
	return nil
}

func handleSettingsError(err error) error {
	if err == nil {
		return nil
	}
	code := "VALIDATION_ERROR"
	if jsonOutput {
		_ = config.WriteJSONError(code, err.Error())
		return errSilent
	}
	fmt.Fprintf(os.Stderr, "config: %v\n", err)
	return errSilent
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configUnsetCmd)
}
