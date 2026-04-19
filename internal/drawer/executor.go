package drawer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/autonoco/buttons/internal/battery"
	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/engine"
)

// ExecuteResult is the aggregated return of a drawer press. Mirrors
// engine.Result's envelope so agents see a consistent shape when
// parsing output from either `buttons press` or `buttons drawer press`.
type ExecuteResult struct {
	RunID       string               `json:"run_id"`
	Drawer      string               `json:"drawer"`
	Status      string               `json:"status"` // "ok" | "failed"
	StartedAt   time.Time            `json:"started_at"`
	FinishedAt  time.Time            `json:"finished_at"`
	DurationMs  int64                `json:"duration_ms"`
	Inputs      map[string]any       `json:"inputs,omitempty"`
	Steps       []StepRun    `json:"steps"`
	FailedStep  string               `json:"failed_step,omitempty"`
	Error       *StepError   `json:"error,omitempty"`
}

// Executor runs drawers. It's intentionally small — the heavy
// lifting (per-button env setup, timeouts, kill trees) is done by
// engine.Execute; this type just walks the step array, resolves
// references, and accumulates results.
type Executor struct {
	BtnSvc     *button.Service
	DrawerSvc  *Service
	Batteries  map[string]string // shared env injected into every step
	BatterySvc *battery.Service  // optional; reloaded per press if nil
}

// NewExecutor builds an executor with the default services. Tests
// can construct a zero-value Executor and set fields directly.
func NewExecutor() *Executor {
	return &Executor{
		BtnSvc:    button.NewService(),
		DrawerSvc: NewService(),
	}
}

// Execute runs the drawer in order. On per-step success, the step's
// output is added to the resolution context under its id (so later
// steps can reference ${<id>.output.field}). On failure, the step's
// OnFailure (or drawer-level default) decides whether to retry,
// continue, or stop.
//
// This is the v1 executor: only Kind=="button" steps run; other
// kinds return KIND_NOT_IMPLEMENTED and the drawer fails.
func (e *Executor) Execute(ctx context.Context, d *Drawer, inputValues map[string]any) (*ExecuteResult, error) {
	runID, err := newRunID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate run id: %w", err)
	}

	started := time.Now().UTC()
	res := &ExecuteResult{
		RunID:     runID,
		Drawer:    d.Name,
		StartedAt: started,
		Inputs:    inputValues,
		Steps:     make([]StepRun, 0, len(d.Steps)),
	}

	// Validate required inputs up front. Missing values fail the run
	// before we touch any buttons — atomic "all or nothing" feel.
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

	// Build initial resolution context with inputs only.
	ctxMap := Context{
		"inputs": toAnyMap(inputValues),
	}

	// Lazy battery load — only if the executor didn't get one via
	// NewExecutorWithBatteries. Silent fallback to empty env on read
	// errors; user can debug via the CLI's batteries command.
	if e.Batteries == nil {
		e.Batteries = map[string]string{}
	}

	for i, step := range d.Steps {
		stepRes, err := e.runStep(ctx, d, &step, ctxMap)
		res.Steps = append(res.Steps, stepRes)
		if err != nil {
			res.Status = "failed"
			res.FailedStep = step.ID
			res.Error = stepRes.Error
			e.finalize(res, d)
			return res, nil
		}

		// On per-step success, expose its output to downstream refs.
		ctxMap[step.ID] = map[string]any{"output": stepRes.Output}
		_ = i
	}

	res.Status = "ok"
	e.finalize(res, d)
	return res, nil
}

// runStep executes one step with retry/failure policy applied.
// Returns the StepRun record + any fatal error (nil on success).
func (e *Executor) runStep(ctx context.Context, d *Drawer, step *Step, ctxMap Context) (StepRun, error) {
	sr := StepRun{ID: step.ID}

	kind := step.Kind
	if kind == "" {
		kind = "button"
	}
	if kind != "button" {
		sr.Status = "failed"
		sr.Error = &StepError{
			Code:        "KIND_NOT_IMPLEMENTED",
			Message:     fmt.Sprintf("step kind %q is reserved but not executable in v1", kind),
			Remediation: "only kind=button steps run today; remove or change this step",
		}
		return sr, fmt.Errorf("kind not implemented")
	}

	btn, err := e.BtnSvc.Get(step.Button)
	if err != nil {
		sr.Status = "failed"
		sr.Error = &StepError{
			Code:        "BUTTON_NOT_FOUND",
			Message:     fmt.Sprintf("button %q does not exist", step.Button),
			Remediation: fmt.Sprintf("run `buttons create %s ...` or fix the step.button reference", step.Button),
		}
		return sr, err
	}
	sr.Button = btn.Name

	// Resolve args against the current context. ${...} refs turn into
	// concrete values; literals pass through. Type coercion happens
	// next when we flatten into ParsePressArgs's string format.
	resolvedArgs := map[string]any{}
	for k, v := range step.Args {
		r, err := Resolve(v, ctxMap)
		if err != nil {
			sr.Status = "failed"
			sr.Error = &StepError{
				Code:        "RESOLVE_ERROR",
				Message:     err.Error(),
				Remediation: "check the ${ref} paths in this step's args",
			}
			return sr, err
		}
		resolvedArgs[k] = r
	}

	// Fill unwired required args from drawer-level inputs by name
	// match. Lets an agent write:
	//
	//   buttons drawer hello press name=world
	//
	// and have `name` land on every step that takes a `name` arg,
	// as long as the arg isn't already wired from an upstream step.
	// This is the "drawer inputs are whatever's left unconnected"
	// philosophy — explicit wiring wins, name-match fills the rest.
	inputsFromCtx, _ := ctxMap["inputs"].(map[string]any)
	for _, a := range btn.Args {
		if _, set := resolvedArgs[a.Name]; set {
			continue
		}
		if v, ok := inputsFromCtx[a.Name]; ok {
			resolvedArgs[a.Name] = v
		}
	}
	sr.Args = resolvedArgs

	// Flatten to the string-map shape engine.Execute expects (via
	// ParsePressArgs). Complex values get JSON-encoded so the button
	// can parse them back.
	argList := make([]string, 0, len(resolvedArgs))
	for k, v := range resolvedArgs {
		argList = append(argList, k+"="+flatten(v))
	}
	parsed, err := button.ParsePressArgs(argList, btn.Args)
	if err != nil {
		sr.Status = "failed"
		sr.Error = &StepError{
			Code:        "VALIDATION_ERROR",
			Message:     err.Error(),
			Remediation: "arg shape doesn't match the button's declared args",
		}
		return sr, err
	}

	codePath, _ := e.BtnSvc.CodePath(step.Button)

	// Honor per-step timeout if set; otherwise engine.Execute uses
	// the button's declared timeout.
	timeout := btn.TimeoutSeconds
	if step.TimeoutSeconds > 0 {
		timeout = step.TimeoutSeconds
	}
	stepCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	policy := step.OnFailure
	attempts := 1
	if policy != nil && policy.Action == "retry" && policy.MaxAttempts > 0 {
		attempts = policy.MaxAttempts
	}

	var engineResult *engine.Result
	for attempt := 1; attempt <= attempts; attempt++ {
		engineResult = engine.Execute(stepCtx, btn, parsed, e.Batteries, nil, codePath)
		if engineResult.Status == "ok" {
			break
		}
		if attempt < attempts {
			// Retry respects retry_on filter.
			if policy != nil && len(policy.RetryOn) > 0 && !containsStr(policy.RetryOn, engineResult.ErrorType) {
				break // error not eligible for retry
			}
			backoff := computeBackoff(policy, attempt)
			if backoff > 0 {
				select {
				case <-stepCtx.Done():
					break
				case <-time.After(backoff):
				}
			}
		}
	}

	sr.ExitCode = engineResult.ExitCode
	sr.DurationMs = engineResult.DurationMs
	sr.Stdout = engineResult.Stdout
	sr.Stderr = engineResult.Stderr
	sr.Status = engineResult.Status

	if engineResult.Status != "ok" {
		sr.Error = &StepError{
			Code:        firstNonEmpty(engineResult.ErrorType, "SCRIPT_ERROR"),
			Message:     firstNonEmpty(strings.TrimSpace(engineResult.Stderr), "step failed"),
			Remediation: "inspect stderr; rerun with `buttons press` to iterate",
		}
		// Honor continue-on-failure: caller decides whether this is
		// fatal to the whole drawer based on policy.
		if policy != nil && policy.Action == "continue" {
			return sr, nil
		}
		return sr, fmt.Errorf("step %s failed", step.ID)
	}

	// Parse stdout as JSON for the output context. If it isn't JSON,
	// fall back to the raw string — buttons that produce free-form
	// stdout still work, they just can't be referenced as .output.<field>.
	var out any
	if strings.TrimSpace(engineResult.Stdout) == "" {
		out = nil
	} else if err := json.Unmarshal([]byte(engineResult.Stdout), &out); err != nil {
		out = engineResult.Stdout
	}
	sr.Output = out
	return sr, nil
}

// finalize fills in timing and persists the run history. Secret
// inputs are redacted before writing to disk.
func (e *Executor) finalize(res *ExecuteResult, d *Drawer) {
	res.FinishedAt = time.Now().UTC()
	res.DurationMs = res.FinishedAt.Sub(res.StartedAt).Milliseconds()

	redacted := RedactSecretInputs(d.Inputs, res.Inputs)
	_ = RecordRun(Run{
		DrawerName: d.Name,
		RunID:      res.RunID,
		StartedAt:  res.StartedAt,
		FinishedAt: res.FinishedAt,
		DurationMs: res.DurationMs,
		Status:     res.Status,
		Inputs:     redacted,
		Steps:      res.Steps,
		ErrorType:  errorCode(res.Error),
	})
}

// computeBackoff turns a retry policy + attempt number into a wait
// duration. Exponential with optional jitter is the only strategy
// v1 implements; "fixed" uses InitialMs.
func computeBackoff(policy *ErrorPolicy, attempt int) time.Duration {
	if policy == nil || policy.Backoff == nil {
		return 0
	}
	b := policy.Backoff
	if b.InitialMs <= 0 {
		return 0
	}
	base := time.Duration(b.InitialMs) * time.Millisecond
	if b.Strategy == "exponential" {
		factor := b.Factor
		if factor <= 0 {
			factor = 2
		}
		mult := 1.0
		for i := 1; i < attempt; i++ {
			mult *= factor
		}
		base = time.Duration(float64(base) * mult)
	}
	if b.MaxMs > 0 && base > time.Duration(b.MaxMs)*time.Millisecond {
		base = time.Duration(b.MaxMs) * time.Millisecond
	}
	// Jitter: ignored in v1 to keep test determinism simple; CEL-era
	// stage 2 can add it.
	return base
}

// --- helpers ---

func newRunID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func toAnyMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func flatten(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64, int, int64:
		return fmt.Sprintf("%v", t)
	default:
		// Anything complex (maps, arrays) round-trips through JSON.
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func firstNonEmpty(xs ...string) string {
	for _, s := range xs {
		if s != "" {
			return s
		}
	}
	return ""
}

func errorCode(e *StepError) string {
	if e == nil {
		return ""
	}
	return e.Code
}
