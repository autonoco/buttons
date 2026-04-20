package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/drawer"
	"github.com/autonoco/buttons/internal/history"
)

var logsFailed bool
var logsLimit int
var logsFollow bool

var logsCmd = &cobra.Command{
	Use:   "logs [name]",
	Short: "View past runs for a button or tail the live progress stream",
	Long: `Structured run history. CLI only — for the full-screen
interactive viewer, use 'buttons board'.

  buttons BUTTONNAME logs            — past runs for this button
  buttons BUTTONNAME logs --follow   — tail live progress JSONL as it writes
  buttons BUTTONNAME logs --failed   — just failures
  buttons BUTTONNAME logs --limit 10 — how many (default 20)
  buttons drawer DRAWERNAME logs     — past runs for this drawer
  buttons logs                       — recent failures across the workspace

--follow streams the latest press's progress events to stdout as
plain text (JSONL: one event per line). No TUI. Pipe it, parse it,
interrupt it with ctrl+C. Use this when an agent needs to watch a
long-running press live.

Agent mode (--json or non-TTY) returns the full Run shape for
non-follow calls. TTY mode prints a compact one-line-per-run table.
The verb-first form (buttons logs NAME) still works as an alias.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runLogs,
}

func runLogs(cmd *cobra.Command, args []string) error {
	// No name → workspace-wide recent failures. Same bucket
	// `buttons summary --json` surfaces, but as the direct triage
	// entry point.
	if len(args) == 0 {
		return logsWorkspaceFailures()
	}
	// --follow → tail the latest press's progress JSONL as plain
	// text on stdout. Same behavior as `buttons tail NAME -f` but
	// under the logs verb so agents find it where they'd look.
	// No TUI, no alt-screen; pipes cleanly.
	if logsFollow {
		return logsFollowStream(args[0])
	}
	// Per-button → structured past runs.
	return logsButtonPast(args[0])
}

// logsFollowStream resolves the latest progress JSONL for a button
// and tails it. Reuses the tail.go helpers so the CLI surface stays
// consistent (logs --follow == tail -f against the same file).
func logsFollowStream(name string) error {
	path, err := resolveTailPath(name)
	if err != nil {
		return handleServiceError(err)
	}
	return streamFile(path, true)
}

// logsButtonPast prints past runs for one button. Honors --failed
// and --limit. Agents get JSON (via --json or non-TTY auto-detect);
// humans get a compact table. Never opens a TUI — that's what
// `buttons board` is for.
func logsButtonPast(name string) error {
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
// button + drawer so agents can triage in one tool call.
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

	allButtonRuns, _ := history.ListAll(n * 4)
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

	if jsonOutput {
		return config.WriteJSON(out)
	}
	if len(out) == 0 {
		fmt.Fprintln(os.Stderr, "no recent failures")
		return nil
	}
	for _, f := range out {
		fmt.Printf("%s  %s  %s\n", f.Target, f.Status, f.ErrorType)
	}
	return nil
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
	logsCmd.Flags().BoolVar(&logsFailed, "failed", false, "only return runs that failed")
	logsCmd.Flags().IntVar(&logsLimit, "limit", 20, "max runs to return")
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "stream the latest press's progress events live (agent-friendly, no TUI)")
	rootCmd.AddCommand(logsCmd)
}
