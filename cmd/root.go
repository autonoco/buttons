package cmd

import (
	"errors"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/config"
	"github.com/spf13/cobra"
)

// version, commit, and date live in cmd/version.go — they're injected
// at build time via -ldflags from the Makefile.

var jsonOutput bool
var noInput bool

// errSilent is returned when the error has already been printed (e.g. as JSON).
// Cobra will not print it again, but Execute() will exit non-zero.
var errSilent = fmt.Errorf("silent error")

var rootCmd = &cobra.Command{
	Use:   "buttons",
	Short: "Deterministic workflow engine for agents",
	Long:  "Buttons gives agents deterministic escape hatches. Create, compose, and execute self-contained actions with clear inputs, outputs, and status.",
	SilenceErrors:         true,
	SilenceUsage:          true,
	DisableFlagParsing:    false,
	TraverseChildren:      true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if !jsonOutput {
			jsonOutput = config.IsNonTTY()
		}
		return config.EnsureDataDir()
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		// If an arg was passed that isn't a subcommand, check if it's a button name
		if len(args) > 0 {
			return showButtonDetail(args[0])
		}

		// No args: show the board (list all buttons for now)
		svc := button.NewService()
		buttons, err := svc.List()
		if err != nil {
			return handleServiceError(err)
		}

		if jsonOutput {
			return config.WriteJSON(buttons)
		}

		if len(buttons) == 0 {
			fmt.Fprintln(os.Stderr, "No buttons found. Create one with: buttons create <name> --code '<script>'")
			return nil
		}

		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tDESCRIPTION\tRUNTIME\tARGS")
		for _, btn := range buttons {
			argSummary := ""
			for i, arg := range btn.Args {
				if i > 0 {
					argSummary += ", "
				}
				argSummary += arg.Name
				if arg.Required {
					argSummary += "*"
				}
			}
			desc := btn.Description
			if desc == "" {
				desc = "-"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", btn.Name, desc, btn.Runtime, argSummary)
		}
		return w.Flush()
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		if !errors.Is(err, errSilent) {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}

func handleServiceError(err error) error {
	se, ok := err.(*button.ServiceError)
	if !ok {
		if jsonOutput {
			fmt.Fprintf(os.Stderr, "internal error: %v\n", err)
			_ = config.WriteJSONError("INTERNAL_ERROR", "an unexpected error occurred")
			return errSilent
		}
		return err
	}
	if jsonOutput {
		_ = config.WriteJSONError(se.Code, se.Message)
		return errSilent
	}
	return fmt.Errorf("%s: %s", se.Code, se.Message)
}

// exactArgs returns a PositionalArgs validator like cobra.ExactArgs but with
// a human-friendly error message that includes usage hints.
func exactArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) == n {
			return nil
		}
		if len(args) == 0 {
			return fmt.Errorf("requires a name argument, see '%s --help'", cmd.CommandPath())
		}
		return fmt.Errorf("expected %d argument(s), got %d, see '%s --help'", n, len(args), cmd.CommandPath())
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "output in JSON format")
	rootCmd.PersistentFlags().BoolVar(&noInput, "no-input", false, "disable all interactive prompts")
	rootCmd.Version = version
}
