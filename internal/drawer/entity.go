// Package drawer defines workflow chains of buttons. A drawer lists
// buttons as ordered steps and wires their outputs into downstream
// step args via ${step_id.output.field} references.
//
// The spec is deliberately schema-first: the Go struct here is the
// single source of truth, and a JSON Schema is generated from it via
// internal/tools/schemagen so agents and humans can both validate
// drawer files at author time.
package drawer

import "time"

// SchemaVersion is the current drawer.json schema version. Bump when
// making breaking changes; additive changes (new optional fields)
// don't require a bump.
const SchemaVersion = 1

// Drawer is the top-level spec stored at
// ~/.buttons/drawers/<name>/drawer.json.
type Drawer struct {
	SchemaVersion int        `json:"schema_version" jsonschema:"const=1,description=Drawer spec version"`
	Name          string     `json:"name" jsonschema:"description=Drawer name (kebab-case)"`
	Description   string     `json:"description,omitempty" jsonschema:"description=Human-readable one-liner"`
	Inputs        []InputDef `json:"inputs,omitempty" jsonschema:"description=Top-level inputs supplied at press time"`
	Steps         []Step     `json:"steps" jsonschema:"description=Ordered list of steps (depth-first execution)"`
	// OnError points at another drawer that runs when any step in this
	// drawer fails. Reserved in the schema now so adding it later
	// doesn't require a schema bump; the v1 executor ignores it.
	OnError *ErrorHandler `json:"on_error,omitempty" jsonschema:"description=Drawer to run on unhandled step failure"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// InputDef declares a drawer-level input. Mirrors button.ArgDef so
// agents see a consistent shape across both, plus a Secret flag that
// drives trace redaction.
type InputDef struct {
	Name        string   `json:"name" jsonschema:"description=Input name (referenced as ${inputs.<name>})"`
	Type        string   `json:"type" jsonschema:"enum=string,enum=int,enum=bool,enum=enum"`
	Required    bool     `json:"required,omitempty"`
	Description string   `json:"description,omitempty"`
	// Values is the allowed value set for Type == "enum".
	Values []string `json:"values,omitempty"`
	// Secret-flagged inputs are redacted from execution traces and
	// never materialized into the pressed/*.json history.
	Secret bool `json:"secret,omitempty" jsonschema:"description=Redact this input from traces"`
}

// Step is one position in the drawer. The Kind field selects how the
// step is interpreted by the executor:
//
//	"button" (default) — invoke Button with Args resolved against context
//	"switch" / "split" / "merge" / "for_each" / "wait" / "drawer" /
//	"transform" / "batch" — reserved for future executor kinds;
//	the v1 runtime errors with KIND_NOT_IMPLEMENTED on these
//
// Reserving these upfront keeps drawer.json forward-compatible so
// agents can author drawers today that will become runnable as new
// kinds ship.
type Step struct {
	ID   string `json:"id" jsonschema:"description=Unique step id (referenced as ${<id>.output.*})"`
	Kind string `json:"kind,omitempty" jsonschema:"enum=button,enum=switch,enum=split,enum=merge,enum=for_each,enum=wait,enum=drawer,enum=transform,enum=batch,description=Step kind (default: button)"`

	// Button is the target when Kind == "button" (the v1 case).
	Button string `json:"button,omitempty" jsonschema:"description=Button name to invoke (kind=button only)"`

	// Args maps the target button's arg names to either literal
	// values or ${...} reference strings. Resolved at press time.
	// Stored as map[string]any so literal ints/bools round-trip
	// without string coercion.
	Args map[string]any `json:"args,omitempty" jsonschema:"description=Arg values or ${ref} strings to pass to the button"`

	// OnFailure overrides the drawer-level failure behavior for this
	// step only. See ErrorPolicy docs.
	OnFailure *ErrorPolicy `json:"on_failure,omitempty"`

	// TimeoutSeconds overrides the button's declared timeout for this
	// step only. 0 means "use the button's timeout."
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`

	// Bindings is a legacy flat map from the drawer v0 stub. Retained
	// so older drawer.json files load; new writes use Args instead.
	Bindings map[string]string `json:"bindings,omitempty"`
}

// ErrorPolicy controls how a step failure is handled. Mirrors
// Temporal's retry policy shape, trimmed to what the v1 executor
// will honor.
type ErrorPolicy struct {
	// Action: "stop" (default) | "continue" | "retry"
	Action string `json:"action" jsonschema:"enum=stop,enum=continue,enum=retry"`

	// MaxAttempts applies to action=retry. 1 = no retry.
	MaxAttempts int `json:"max_attempts,omitempty"`

	// Backoff applies to action=retry. Omitted means no delay.
	Backoff *Backoff `json:"backoff,omitempty"`

	// RetryOn narrows retry to specific error codes (TIMEOUT,
	// RATE_LIMIT, etc). Empty means "retry on any error".
	RetryOn []string `json:"retry_on,omitempty"`

	// CompensateWith is reserved for the saga/compensation pattern
	// (v2+). The v1 executor ignores this field. It's schema-only so
	// drawers authored today can declare cleanup intent that becomes
	// active when the compensation executor lands.
	CompensateWith string `json:"compensate_with,omitempty"`
}

// Backoff describes retry delay shape. Exponential with optional
// jitter is the only strategy v1 supports; "fixed" reserved.
type Backoff struct {
	Strategy  string `json:"strategy" jsonschema:"enum=exponential,enum=fixed"`
	InitialMs int    `json:"initial_ms,omitempty"`
	Factor    float64 `json:"factor,omitempty"`
	MaxMs     int    `json:"max_ms,omitempty"`
	Jitter    bool   `json:"jitter,omitempty"`
}

// ErrorHandler points at another drawer to invoke on failure. The
// target drawer must accept an input schema compatible with the
// standard error payload (drawer, run_id, failed_step, error, inputs).
type ErrorHandler struct {
	Drawer string         `json:"drawer"`
	With   map[string]any `json:"with,omitempty"`
}
