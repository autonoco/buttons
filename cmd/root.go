package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

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
		// NAME-first verb form matching `buttons drawer NAME add/...`
		//
		//   buttons BUTTONNAME logs [--follow] [--failed] [--limit N]
		//
		// Routes through the existing logs command so every flag
		// keeps working.
		if len(args) >= 2 && args[1] == "logs" {
			return runLogs(cmd, []string{args[0]})
		}
		// If a positional arg was passed and it isn't a subcommand,
		// fall back to per-button detail — preserves existing
		// `buttons <name>` shorthand.
		if len(args) > 0 {
			return showButtonDetail(args[0])
		}
		// No args: show the workspace summary. One tool call, full
		// orientation — the canonical agent cold-start command.
		return runSummary()
	},
}

// Root returns the root command for use by doc generators and tests.
func Root() *cobra.Command { return rootCmd }

func Execute() {
	// NAME-first rewriting. Cobra routes by subcommand name in arg
	// position 1, so `buttons BUTTONNAME logs ...` doesn't reach
	// logsCmd naturally. We rewrite it to `buttons logs BUTTONNAME
	// ...` before Cobra sees it. Same pattern intentionally NOT
	// applied to `buttons drawer NAME logs` because drawerCmd
	// already does its own NAME-first dispatch internally.
	if len(os.Args) >= 3 && os.Args[2] == "logs" && !strings.HasPrefix(os.Args[1], "-") {
		switch os.Args[1] {
		case "drawer", "create", "press", "list", "delete", "rm", "remove",
			"batteries", "board", "config", "history", "init", "logs",
			"smash", "store", "summary", "tail", "update", "version":
			// Already a subcommand; don't rewrite.
		default:
			rewritten := make([]string, 0, len(os.Args))
			rewritten = append(rewritten, os.Args[0], "logs", os.Args[1])
			rewritten = append(rewritten, os.Args[3:]...)
			os.Args = rewritten
		}
	}

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

// printNextHint emits a single muted "Next: ..." line to stderr so a
// human in the terminal sees the common follow-up after a successful
// command — the idea is: don't make users re-`--help` to find the
// next move.
//
// Skipped in --json mode (machines don't need prose hints) and when
// stdout is piped (if you're feeding output into a pipeline you
// probably don't want chatter either). Using Fprintln keeps the line
// on stderr so stdout stays clean for piping.
func printNextHint(format string, args ...any) {
	if jsonOutput || config.IsNonTTY() {
		return
	}
	fmt.Fprintf(os.Stderr, "  Next: "+format+"\n", args...)
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
