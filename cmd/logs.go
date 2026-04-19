package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/drawer"
	"github.com/autonoco/buttons/internal/history"
	"github.com/autonoco/buttons/internal/tui"
)

var logsArgs []string
var logsFailed bool
var logsLimit int
var logsFollow bool

var logsCmd = &cobra.Command{
	Use:   "logs [name]",
	Short: "View a button's past runs, or press and stream live",
	Long: `View a button's run history. Preferred form is name-first to
match the rest of the CLI:

  buttons BUTTONNAME logs           — past runs for this button
  buttons BUTTONNAME logs --follow  — press + stream live
  buttons BUTTONNAME logs --failed  — just failures
  buttons drawer DRAWERNAME logs    — past runs for this drawer

The verb-first form (buttons logs NAME) still works as an alias.
buttons logs (no name) dumps recent failures across every button
and drawer — same shape as summary.recent_failures.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runLogs,
}

func runLogs(cmd *cobra.Command, args []string) error {
	// Workspace-level: no name → recent failures across everything.
	if len(args) == 0 {
		return logsWorkspaceFailures()
	}

	// Per-button: if --follow AND TTY AND not --json, drop into the
	// live-stream TUI. Otherwise return the structured past-runs
	// view — this is the agent path and also the default non-TTY
	// path. Failures live in history, so `buttons NAME logs --failed`
	// is the triage call.
	if logsFollow && !jsonOutput && !config.IsNonTTY() {
		return runLogsTUI(cmd, args[0])
	}
	return logsButtonJSON(args[0])
}

func runLogsTUI(cmd *cobra.Command, name string) error {
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

// logsButtonJSON prints past runs for one button. Honors --failed
// (filter to non-ok) and --limit (default 20). Agents get full
// structured JSON; humans in TTY get a compact table. Either way
// it's the standard CLI — no TUI unless the caller asks for
// --follow.
func logsButtonJSON(name string) error {
	n := logsLimit
	if n <= 0 {
		n = 20
	}
	runs, err := history.List(name, n)
	if err != nil {
		return handleServiceError(err)
	}
	if logsFailed {
		kept := runs[:0]
		for _, r := range runs {
			if r.Status != "ok" {
				kept = append(kept, r)
			}
		}
		runs = kept
	}
	if jsonOutput {
		return config.WriteJSON(runs)
	}
	if len(runs) == 0 {
		fmt.Fprintf(os.Stderr, "no runs for %s yet\n", name)
		return nil
	}
	for _, r := range runs {
		status := r.Status
		if r.ErrorType != "" {
			status = r.Status + " · " + r.ErrorType
		}
		fmt.Printf("%s  %s  exit=%d  %dms\n",
			r.StartedAt.Local().Format("2006-01-02 15:04:05"),
			status, r.ExitCode, r.DurationMs)
	}
	return nil
}

// logsWorkspaceFailures aggregates recent failures across every
// button + drawer so agents can triage in one tool call. Same bucket
// that `buttons summary --json` surfaces under recent_failures, but
// this command is the "show me what broke" direct path.
func logsWorkspaceFailures() error {
	n := logsLimit
	if n <= 0 {
		n = 20
	}

	type failure struct {
		Target     string `json:"target"`
		RunID      string `json:"run_id,omitempty"`
		StartedAt  any    `json:"started_at"`
		Status     string `json:"status"`
		ExitCode   int    `json:"exit_code,omitempty"`
		ErrorType  string `json:"error_type,omitempty"`
		Stderr     string `json:"stderr,omitempty"`
		FailedStep string `json:"failed_step,omitempty"`
	}

	out := []failure{}

	// Button runs.
	allButtonRuns, _ := history.ListAll(n * 4) // overfetch; filter below
	for _, r := range allButtonRuns {
		if r.Status == "ok" {
			continue
		}
		out = append(out, failure{
			Target:    "button/" + r.ButtonName,
			StartedAt: r.StartedAt,
			Status:    r.Status,
			ExitCode:  r.ExitCode,
			ErrorType: r.ErrorType,
			Stderr:    truncateForSummary(r.Stderr, 400),
		})
		if len(out) >= n {
			break
		}
	}

	// Drawer runs — scan each drawer's history.
	if len(out) < n {
		dsvc := drawer.NewService()
		drawers, _ := dsvc.List()
		for _, d := range drawers {
			runs, _ := drawer.ListRuns(d.Name, n)
			for _, r := range runs {
				if r.Status == "ok" {
					continue
				}
				out = append(out, failure{
					Target:     "drawer/" + d.Name,
					RunID:      r.RunID,
					StartedAt:  r.StartedAt,
					Status:     r.Status,
					ErrorType:  r.ErrorType,
					FailedStep: lastFailedStep(r),
				})
				if len(out) >= n {
					break
				}
			}
			if len(out) >= n {
				break
			}
		}
	}

	return config.WriteJSON(out)
}

func lastFailedStep(r drawer.Run) string {
	for i := len(r.Steps) - 1; i >= 0; i-- {
		if r.Steps[i].Status != "ok" {
			return r.Steps[i].ID
		}
	}
	return ""
}

func truncateForSummary(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func init() {
	logsCmd.Flags().StringArrayVar(&logsArgs, "arg", nil, "argument as key=value (with --follow, passed through to the press)")
	logsCmd.Flags().BoolVar(&logsFailed, "failed", false, "only return runs that failed")
	logsCmd.Flags().IntVar(&logsLimit, "limit", 20, "max runs to return")
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "press the button and stream live output in a TUI")
	rootCmd.AddCommand(logsCmd)
}
