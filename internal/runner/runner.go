// Package runner presses buttons programmatically — the shared core behind
// `buttons press`, `buttons serve` (#270), and `buttons mcp` (#265). It mirrors
// the press command: validate args against the spec, load batteries, honor the
// button's queue, execute, and (optionally) record history. Centralizing this
// keeps every surface on the same execution + security contract.
package runner

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/autonoco/buttons/internal/battery"
	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/engine"
	"github.com/autonoco/buttons/internal/history"
	"github.com/autonoco/buttons/internal/queue"
)

// Options tunes a programmatic press.
type Options struct {
	// TimeoutSeconds overrides the button's configured timeout (0 = use the
	// button's own TimeoutSeconds).
	TimeoutSeconds int
	// MaxTimeoutSeconds, when > 0, is a hard ceiling on the effective timeout —
	// MCP enforces 120s regardless of what the button spec asks for.
	MaxTimeoutSeconds int
	// RecordHistory persists the run to the button's pressed/ dir (servers and
	// MCP want this on; a dry caller can leave it off).
	RecordHistory bool
}

// Press runs the named button with already-supplied string args. It returns the
// engine result and a non-nil error only for pre-flight failures (button
// missing, invalid args, queue acquisition timeout). A button that runs and
// exits non-zero is a *successful* Press whose result.Status is "error" — the
// caller inspects result.Status to distinguish.
func Press(ctx context.Context, name string, args map[string]string, opts Options) (*engine.Result, error) {
	svc := button.NewService()
	btn, err := svc.Get(name)
	if err != nil {
		return nil, err
	}

	// Validate args against the spec (required/enum/type) via the same parser
	// the CLI uses, so every surface rejects bad input identically.
	raws := make([]string, 0, len(args))
	for k, v := range args {
		raws = append(raws, k+"="+v)
	}
	parsed, err := button.ParsePressArgs(raws, btn.Args)
	if err != nil {
		return nil, err
	}

	// Resolve timeout: override → button default, then clamp to the hard cap.
	timeout := btn.TimeoutSeconds
	if opts.TimeoutSeconds > 0 {
		timeout = opts.TimeoutSeconds
	}
	if opts.MaxTimeoutSeconds > 0 && (timeout <= 0 || timeout > opts.MaxTimeoutSeconds) {
		timeout = opts.MaxTimeoutSeconds
	}

	runCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer cancel()
	}

	// Resolve the code/prompt path for non-HTTP buttons.
	var codePath string
	if btn.URL == "" {
		if btn.Runtime == "prompt" {
			dir, derr := config.ButtonDir(btn.Name)
			if derr != nil {
				return nil, derr
			}
			codePath = filepath.Join(dir, "AGENT.md")
		} else {
			codePath, err = svc.CodePath(btn.Name)
			if err != nil {
				return nil, err
			}
		}
	}

	batteries, err := loadBatteries()
	if err != nil {
		return nil, err
	}

	// Honor a declared queue/concurrency limit, same as `buttons press`.
	if btn.Queue != nil && btn.Queue.Name != "" {
		deadline, ok := runCtx.Deadline()
		if !ok {
			deadline = time.Now().Add(time.Hour)
		}
		key := btn.Queue.Key
		for k, v := range parsed {
			key = strings.ReplaceAll(key, "${inputs."+k+"}", v)
			key = strings.ReplaceAll(key, "${args."+k+"}", v)
		}
		lock, qerr := queue.Acquire(queue.Config{Name: btn.Queue.Name, Concurrency: btn.Queue.Concurrency, Key: key}, 200*time.Millisecond, deadline)
		if qerr != nil {
			return nil, qerr
		}
		defer lock.Release()
	}

	result := engine.Execute(runCtx, btn, parsed, batteries, nil, codePath)

	if opts.RecordHistory {
		_ = history.Record(result)
	}
	return result, nil
}

// loadBatteries resolves BUTTONS_BAT_<KEY> values for the press, matching the
// CLI's battery scope rules (local-if-project, else global).
func loadBatteries() (map[string]string, error) {
	svc, err := battery.NewServiceFromEnv(func() (string, bool) {
		if !config.IsProjectLocal() {
			return "", false
		}
		dir, err := config.DataDir()
		if err != nil {
			return "", false
		}
		return dir, true
	})
	if err != nil {
		return nil, err
	}
	return svc.Env()
}
