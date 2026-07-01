package cmd

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/autonoco/buttons/internal/agentdoc"
	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/engine"
	"github.com/autonoco/buttons/internal/history"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

var (
	smashOnFailure   string
	smashConcurrency int
	smashTimeout     int
)

// smashMaxConcurrency is a hard ceiling on simultaneous presses regardless of
// --concurrency, so `smash` can't be turned into a fork bomb.
const smashMaxConcurrency = 50

var smashCmd = &cobra.Command{
	Use:   "smash [buttons...]",
	Short: "Run multiple buttons in parallel",
	Long: `Run several buttons concurrently. Names may be comma- or space-separated:

  buttons smash a,b,c
  buttons smash a b c

  --on-failure continue   run them all, report failures at the end (default)
  --on-failure stop       cancel the remaining buttons on the first failure
  --concurrency N         max buttons running at once (0 = NumCPU; hard cap 50)
  --timeout SECS          per-button timeout override

JSON mode returns an array of per-button results; every run is recorded in
history. Exits non-zero if any button failed.`,
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: completeButtonNames,
	RunE:              runSmash,
}

func runSmash(_ *cobra.Command, args []string) error {
	names := splitButtonNames(args)
	if len(names) == 0 {
		return fmt.Errorf("no buttons named")
	}
	if smashOnFailure != "stop" && smashOnFailure != "continue" {
		return fmt.Errorf("--on-failure must be 'stop' or 'continue', got %q", smashOnFailure)
	}

	limit := smashConcurrency
	if limit <= 0 {
		limit = runtime.NumCPU()
	}
	if limit > smashMaxConcurrency {
		limit = smashMaxConcurrency // fork-bomb guard
	}

	// Load batteries once; every press shares the same injected env.
	batSvc, err := newBatteryService()
	if err != nil {
		return handleServiceError(err)
	}
	batteries, err := batSvc.Env()
	if err != nil {
		return handleServiceError(err)
	}

	type slot struct {
		Name   string
		Result *engine.Result
		Err    string
	}
	results := make([]slot, len(names))
	for i, n := range names {
		results[i].Name = n
	}

	g, gctx := errgroup.WithContext(context.Background())
	g.SetLimit(limit)
	for i := range names {
		i := i
		g.Go(func() error {
			res, perr := smashPress(gctx, names[i], batteries, smashTimeout)
			if perr != nil {
				results[i].Err = perr.Error()
				if smashOnFailure == "stop" {
					return perr // cancels gctx → in-flight siblings stop
				}
				return nil
			}
			results[i].Result = res
			if res.Status != "ok" && smashOnFailure == "stop" {
				return fmt.Errorf("%s: %s", names[i], res.Status)
			}
			return nil
		})
	}
	_ = g.Wait() // per-button outcomes captured in results; we report them all

	failures := 0
	for _, s := range results {
		if s.Err != "" || (s.Result != nil && s.Result.Status != "ok") {
			failures++
		}
	}

	if jsonOutput {
		arr := make([]map[string]any, len(results))
		for i, s := range results {
			m := map[string]any{"button": s.Name}
			switch {
			case s.Err != "":
				m["error"] = s.Err
			case s.Result != nil:
				m["result"] = s.Result
			default:
				m["skipped"] = true // cancelled before it started (--on-failure stop)
			}
			arr[i] = m
		}
		if err := config.WriteJSON(map[string]any{"results": arr, "total": len(results), "failures": failures}); err != nil {
			return err
		}
		if failures > 0 {
			return errSilent
		}
		return nil
	}

	for _, s := range results {
		switch {
		case s.Err != "":
			fmt.Fprintf(os.Stderr, "✗ %s: %s\n", s.Name, s.Err)
		case s.Result == nil:
			fmt.Fprintf(os.Stderr, "– %s: skipped (cancelled)\n", s.Name)
		case s.Result.Status == "ok":
			fmt.Fprintf(os.Stderr, "✓ %s (%dms)\n", s.Name, s.Result.DurationMs)
		default:
			fmt.Fprintf(os.Stderr, "✗ %s: %s (exit %d)\n", s.Name, s.Result.Status, s.Result.ExitCode)
		}
	}
	fmt.Fprintf(os.Stderr, "smashed %d button(s), %d failed\n", len(results), failures)
	if failures > 0 {
		return errSilent
	}
	return nil
}

// splitButtonNames flattens comma- and space-separated args into a name list.
func splitButtonNames(args []string) []string {
	var names []string
	for _, a := range args {
		for _, p := range strings.Split(a, ",") {
			if p = strings.TrimSpace(p); p != "" {
				names = append(names, p)
			}
		}
	}
	return names
}

// smashPress runs one button (no follow / idempotency) and records history.
// Mirrors the core of `buttons press` so timing + history stay consistent.
func smashPress(ctx context.Context, name string, batteries map[string]string, timeoutOverride int) (*engine.Result, error) {
	svc := button.NewService()
	btn, err := svc.Get(name)
	if err != nil {
		return nil, err
	}
	timeout := btn.TimeoutSeconds
	if timeoutOverride > 0 {
		timeout = timeoutOverride
	}

	var codePath string
	if btn.URL == "" {
		if btn.Runtime == "prompt" {
			dir, derr := config.ButtonDir(btn.Name)
			if derr != nil {
				return nil, derr
			}
			codePath = agentdoc.Path(dir)
		} else if codePath, err = svc.CodePath(btn.Name); err != nil {
			return nil, err
		}
	}

	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	res := engine.Execute(cctx, btn, map[string]string{}, batteries, nil, codePath)
	if err := history.Record(res); err != nil {
		fmt.Fprintf(os.Stderr, "warning: history for %s: %v\n", name, err)
	}
	return res, nil
}

func init() {
	smashCmd.Flags().StringVar(&smashOnFailure, "on-failure", "continue", "stop | continue")
	smashCmd.Flags().IntVar(&smashConcurrency, "concurrency", 0, "max buttons running at once (0 = NumCPU; hard cap 50)")
	smashCmd.Flags().IntVar(&smashTimeout, "timeout", 0, "per-button timeout override (seconds)")
	rootCmd.AddCommand(smashCmd)
}
