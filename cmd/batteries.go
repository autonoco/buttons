package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/autonoco/buttons/internal/battery"
	"github.com/autonoco/buttons/internal/config"
)

// Flags for the scope-qualified subcommands. --global and --local are
// mutually exclusive; the default scope falls back to "local when in a
// project, global otherwise" so most commands require no flags.
var (
	batteriesFlagGlobal bool
	batteriesFlagLocal  bool
	batteriesFlagReveal bool
)

var batteriesCmd = &cobra.Command{
	Use:   "batteries",
	Short: "Manage environment variables and secrets",
	Long: `Batteries are key/value pairs injected into every button press
as BUTTONS_BAT_<KEY>=<value>. Use them to store API tokens and other
secrets outside your button scripts.

Scopes:
  global   ~/.buttons/batteries.json  — available from every project
  local    .buttons/batteries.json    — only when pressing inside the
                                        project; overrides global on
                                        key collision

List / get read from both scopes (local overrides on collision). Set /
rm target local when inside a project, global otherwise; pass --global
or --local to pick explicitly.

Keys must match [A-Z][A-Z0-9_]* (uppercase, digits, underscore).

Examples:
  buttons batteries set APIFY_TOKEN apify_api_xxx
  buttons batteries set OPENAI_KEY sk-... --global
  buttons batteries list
  buttons batteries get APIFY_TOKEN
  buttons batteries rm OLD_KEY`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var batteriesSetCmd = &cobra.Command{
	Use:   "set KEY VALUE",
	Short: "Set a battery",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		key, value := args[0], args[1]
		svc, err := newBatteryService()
		if err != nil {
			return handleBatteryError(err)
		}
		scope, err := resolveBatteryScope(svc)
		if err != nil {
			return handleBatteryError(err)
		}
		if err := svc.Set(key, value, scope); err != nil {
			return handleBatteryError(err)
		}

		if jsonOutput {
			return config.WriteJSON(map[string]any{
				"key":   key,
				"scope": string(scope),
			})
		}
		fmt.Fprintf(os.Stderr, "set %s (%s)\n", key, scope)
		return nil
	},
}

var batteriesGetCmd = &cobra.Command{
	Use:   "get KEY",
	Short: "Print a battery value",
	Long: `Print the raw value of a battery to stdout. Intended for shell
substitution, e.g. ` + "`curl -H \"Authorization: Bearer $(buttons batteries get APIFY_TOKEN)\" ...`" + `.

Looks up local first (if in a project), then global. In --json mode the
value is still returned raw — redaction only applies to ` + "`list`" + `.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := args[0]
		svc, err := newBatteryService()
		if err != nil {
			return handleBatteryError(err)
		}
		value, scope, err := svc.Get(key)
		if err != nil {
			return handleBatteryError(err)
		}

		if jsonOutput {
			return config.WriteJSON(map[string]any{
				"key":   key,
				"value": value,
				"scope": string(scope),
			})
		}
		fmt.Println(value)
		return nil
	},
}

var batteriesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List every battery",
	Long: `List batteries from every scope. Values are redacted by default —
pass --reveal to print them in full.

Local entries that shadow a global key are shown after the global entry;
at press time the local value wins.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newBatteryService()
		if err != nil {
			return handleBatteryError(err)
		}
		entries, err := svc.List()
		if err != nil {
			return handleBatteryError(err)
		}

		if jsonOutput {
			payload := make([]map[string]any, 0, len(entries))
			for _, e := range entries {
				val := e.Value
				if !batteriesFlagReveal {
					val = battery.Redact(val)
				}
				payload = append(payload, map[string]any{
					"key":   e.Key,
					"value": val,
					"scope": string(e.Scope),
				})
			}
			return config.WriteJSON(payload)
		}

		if len(entries) == 0 {
			fmt.Fprintln(os.Stderr, "no batteries set. try: buttons batteries set KEY value")
			return nil
		}
		for _, e := range entries {
			val := e.Value
			if !batteriesFlagReveal {
				val = battery.Redact(val)
			}
			fmt.Printf("  %-24s  %-8s  %s\n", e.Key, e.Scope, val)
		}
		return nil
	},
}

var batteriesRmCmd = &cobra.Command{
	Use:     "rm KEY",
	Aliases: []string{"remove", "delete"},
	Short:   "Remove a battery",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := args[0]
		svc, err := newBatteryService()
		if err != nil {
			return handleBatteryError(err)
		}
		scope, err := resolveBatteryScope(svc)
		if err != nil {
			return handleBatteryError(err)
		}
		if err := svc.Delete(key, scope); err != nil {
			return handleBatteryError(err)
		}

		if jsonOutput {
			return config.WriteJSON(map[string]any{
				"key":   key,
				"scope": string(scope),
			})
		}
		fmt.Fprintf(os.Stderr, "removed %s (%s)\n", key, scope)
		return nil
	},
}

// newBatteryService resolves the global and (optionally) local paths
// and returns a ready-to-use Service. Uses battery.NewServiceFromEnv
// so tests can inject scratch dirs via BUTTONS_HOME; the project-
// discovery closure keeps the battery package decoupled from the
// config package.
func newBatteryService() (*battery.Service, error) {
	return battery.NewServiceFromEnv(func() (string, bool) {
		if !config.IsProjectLocal() {
			return "", false
		}
		dir, err := config.DataDir()
		if err != nil {
			return "", false
		}
		return dir, true
	})
}

// resolveBatteryScope maps --global / --local / default flag state to a
// battery.Scope. Default is local-if-available, else global — matches
// the "inside a project you usually mean local" intuition.
func resolveBatteryScope(svc *battery.Service) (battery.Scope, error) {
	if batteriesFlagGlobal && batteriesFlagLocal {
		return "", errors.New("cannot pass both --global and --local")
	}
	if batteriesFlagGlobal {
		return battery.ScopeGlobal, nil
	}
	if batteriesFlagLocal {
		return battery.ScopeLocal, nil
	}
	return svc.ResolveDefaultScope(), nil
}

func handleBatteryError(err error) error {
	if err == nil {
		return nil
	}
	code := "VALIDATION_ERROR"
	switch {
	case errors.Is(err, battery.ErrNotFound):
		code = "NOT_FOUND"
	case errors.Is(err, battery.ErrLocalUnavailable):
		code = "MISSING_ARG"
	}
	if jsonOutput {
		_ = config.WriteJSONError(code, err.Error())
		return errSilent
	}
	fmt.Fprintf(os.Stderr, "batteries: %v\n", err)
	return errSilent
}

func init() {
	rootCmd.AddCommand(batteriesCmd)
	batteriesCmd.AddCommand(batteriesSetCmd)
	batteriesCmd.AddCommand(batteriesGetCmd)
	batteriesCmd.AddCommand(batteriesListCmd)
	batteriesCmd.AddCommand(batteriesRmCmd)

	for _, c := range []*cobra.Command{batteriesSetCmd, batteriesRmCmd} {
		c.Flags().BoolVar(&batteriesFlagGlobal, "global", false, "target the global batteries file (~/.buttons/batteries.json)")
		c.Flags().BoolVar(&batteriesFlagLocal, "local", false, "target the project-local batteries file (.buttons/batteries.json)")
	}
	batteriesListCmd.Flags().BoolVar(&batteriesFlagReveal, "reveal", false, "print values in full instead of redacted")
}
