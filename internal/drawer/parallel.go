package drawer

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"runtime"
	"sync"
	"time"
)

// ParallelOptions configure a parallel drawer press.
type ParallelOptions struct {
	// OnFailure is "stop" (default — first failure cancels the in-flight wave)
	// or "continue" (collect every step's result).
	OnFailure string
	// Limit caps concurrent steps. <=0 → runtime.NumCPU().
	Limit int
}

// identToken matches an identifier (incl. hyphens, as step ids allow) so we can
// pull step references out of a ${…} expression.
var identToken = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_-]*`)

// stepDeps returns the set of step ids this step references via ${id…}. It
// marshals the step and scans every ${…} expression for identifiers that name
// another step — robust to complex CEL (ternary, ??, dotted paths).
func stepDeps(step Step, stepIDs map[string]bool) map[string]bool {
	deps := map[string]bool{}
	data, err := json.Marshal(step)
	if err != nil {
		return deps
	}
	for _, m := range refPattern.FindAllStringSubmatch(string(data), -1) {
		expr := m[1] // capture group 1 = inside ${…}; group 2 = $ENV{…}
		if expr == "" {
			continue
		}
		for _, id := range identToken.FindAllString(expr, -1) {
			if id != step.ID && stepIDs[id] {
				deps[id] = true
			}
		}
	}
	return deps
}

// ExecuteParallel runs the drawer's steps concurrently while honoring data
// dependencies: a step that references another step's ${id.output} waits for
// it. Independent steps run together (bounded by Limit). With OnFailure="stop"
// the first failure cancels the in-flight wave; "continue" runs every step and
// reports all failures. Output ordering in the result follows the original step
// order, so the result is deterministic regardless of finish order.
func (e *Executor) ExecuteParallel(ctx context.Context, d *Drawer, inputValues map[string]any, opts ParallelOptions) (*ExecuteResult, error) {
	runID, err := newRunID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate run id: %w", err)
	}
	if opts.OnFailure == "" {
		opts.OnFailure = "stop"
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = runtime.NumCPU()
	}

	started := time.Now().UTC()
	res := &ExecuteResult{
		RunID:     runID,
		Drawer:    d.Name,
		StartedAt: started,
		Inputs:    inputValues,
		Steps:     make([]StepRun, 0, len(d.Steps)),
	}

	// Required-input check, same as the sequential path.
	for _, in := range d.Inputs {
		if in.Required {
			if _, ok := inputValues[in.Name]; !ok {
				res.Status = "failed"
				res.Error = &StepError{
					Code:        "MISSING_INPUT",
					Message:     fmt.Sprintf("drawer input %q is required", in.Name),
					Remediation: fmt.Sprintf("pass %s=<value> to `buttons drawer %s press`", in.Name, d.Name),
				}
				e.finalize(res, d)
				return res, nil
			}
		}
	}

	ctxMap := Context{
		"inputs":   toAnyMap(inputValues),
		"webhooks": e.buildWebhooksMap(),
	}
	if e.Batteries == nil {
		e.Batteries = map[string]string{}
	}

	// Dependency graph.
	stepIDs := make(map[string]bool, len(d.Steps))
	for _, s := range d.Steps {
		stepIDs[s.ID] = true
	}
	deps := make([]map[string]bool, len(d.Steps))
	for i, s := range d.Steps {
		deps[i] = stepDeps(s, stepIDs)
	}

	done := make([]bool, len(d.Steps))
	results := make([]StepRun, len(d.Steps))
	completed := map[string]bool{}
	remaining := len(d.Steps)
	stopped := false

	wctx, cancel := context.WithCancel(ctx)
	defer cancel()

	for remaining > 0 && !stopped {
		// Build the next wave: every not-done step whose deps are all completed.
		wave := make([]int, 0, remaining)
		for i := range d.Steps {
			if done[i] {
				continue
			}
			ready := true
			for dep := range deps[i] {
				if !completed[dep] {
					ready = false
					break
				}
			}
			if ready {
				wave = append(wave, i)
			}
		}
		if len(wave) == 0 {
			// Nothing runnable but steps remain → cycle or a ref to a missing step.
			res.Status = "failed"
			res.Error = &StepError{
				Code:        "PARALLEL_DEADLOCK",
				Message:     "remaining steps have a dependency cycle or reference an unknown step",
				Remediation: "break the cycle, fix the ${ref}, or run without --mode parallel",
			}
			break
		}

		// Run the wave concurrently (bounded). ctxMap is read-only during the
		// wave; outputs are merged after the barrier, so there are no races.
		sem := make(chan struct{}, limit)
		var wg sync.WaitGroup
		for _, idx := range wave {
			wg.Add(1)
			sem <- struct{}{}
			go func(idx int) {
				defer wg.Done()
				defer func() { <-sem }()
				step := d.Steps[idx]
				sr, rerr := e.runStep(wctx, d, &step, ctxMap)
				results[idx] = sr
				if rerr != nil && opts.OnFailure == "stop" {
					cancel() // cancel siblings in this wave
				}
			}(idx)
		}
		wg.Wait()

		// Process wave results in index order (deterministic).
		for _, idx := range wave {
			done[idx] = true
			remaining--
			sr := results[idx]
			if sr.Error != nil || sr.Status == "failed" {
				if opts.OnFailure == "stop" {
					stopped = true
				}
				continue // leave output unmerged on failure
			}
			ctxMap[d.Steps[idx].ID] = map[string]any{"output": sr.Output}
			completed[d.Steps[idx].ID] = true
		}
	}

	// Assemble steps in original order for everything that ran.
	for i := range d.Steps {
		if done[i] {
			res.Steps = append(res.Steps, results[i])
		}
	}

	// Status: failed if any ran step failed (or deadlock set it already).
	if res.Status != "failed" {
		res.Status = "ok"
	}
	for i := range d.Steps {
		if done[i] && (results[i].Error != nil || results[i].Status == "failed") {
			res.Status = "failed"
			if res.FailedStep == "" {
				res.FailedStep = d.Steps[i].ID
				if res.Error == nil {
					res.Error = results[i].Error
				}
			}
		}
	}

	e.finalize(res, d)
	return res, nil
}
