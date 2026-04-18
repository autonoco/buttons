package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/tui"
)

var logsArgs []string

var logsCmd = &cobra.Command{
	Use:   "logs [name]",
	Short: "Press a button and watch its output stream live",
	Long: `Press a button in a full-screen viewer that tails every line of
stdout / stderr as the child writes it.

The viewer stays open after the press completes so you can scroll
the output at leisure. Press esc or q to dismiss; ctrl+c cancels an
in-flight press (the child's process group is killed).

Scope is one press. If the button takes required args, pass them with
--arg key=value the same way 'buttons press' does.

Only shell and code buttons stream today. HTTP and prompt buttons
still use 'buttons press' for now — their execution is request /
response, not a long-running process.

Examples:
  buttons logs deploy
  buttons logs deploy --arg env=staging
  buttons logs etl --arg file=/tmp/x.csv`,
	Args: exactArgs(1),
	RunE: runLogs,
}

func runLogs(cmd *cobra.Command, args []string) error {
	if jsonOutput {
		_ = config.WriteJSONError("NOT_APPLICABLE", "logs is an interactive TUI; --json is not supported")
		return errSilent
	}
	if config.IsNonTTY() {
		// Piped / CI — the TUI can't render. Tell the user to use
		// 'buttons press --json' instead of dropping into a broken state.
		fmt.Fprintln(cmd.ErrOrStderr(), "logs requires a TTY. For programmatic output, use: buttons press --json")
		return errSilent
	}

	name := args[0]
	svc := button.NewService()

	btn, err := svc.Get(name)
	if err != nil {
		return handleServiceError(err)
	}

	// HTTP / prompt buttons don't stream. Surface a clear error instead
	// of showing an empty log viewer forever.
	if btn.URL != "" || btn.Runtime == "prompt" {
		fmt.Fprintf(cmd.ErrOrStderr(),
			"buttons logs: %s is a %s button, which doesn't stream. Use: buttons press %s\n",
			btn.Name, runtimeLabel(btn), btn.Name)
		return errSilent
	}

	parsedArgs, err := button.ParsePressArgs(logsArgs, btn.Args)
	if err != nil {
		return handleServiceError(err)
	}

	codePath, err := svc.CodePath(btn.Name)
	if err != nil {
		return handleServiceError(err)
	}

	// Same battery resolution as cmd/press so a battery added in
	// another shell reaches the child.
	batSvc, err := newBatteryService()
	if err != nil {
		return handleServiceError(err)
	}
	batteries, err := batSvc.Env()
	if err != nil {
		return handleServiceError(err)
	}

	if err := tui.RunLogs(btn, parsedArgs, batteries, codePath); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "logs: %v\n", err)
		return errSilent
	}
	return nil
}

// runtimeLabel returns a user-friendly runtime name for error messages.
// Maps blank / URL / prompt to the intent rather than the internal enum.
func runtimeLabel(btn *button.Button) string {
	if btn.URL != "" {
		return "HTTP"
	}
	if btn.Runtime == "prompt" {
		return "prompt"
	}
	if btn.Runtime == "" {
		return "shell"
	}
	return btn.Runtime
}

func init() {
	logsCmd.Flags().StringArrayVar(&logsArgs, "arg", nil, "argument as key=value (repeatable; validated against the button spec)")
	rootCmd.AddCommand(logsCmd)
}
