package drawer

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/autonoco/buttons/internal/button"
)

// ValidationIssue is a single error or warning surfaced by Validate.
// Distinguish by Severity — agents can ignore warnings for a dry-run
// preview but must clear errors before pressing.
type ValidationIssue struct {
	Severity    string `json:"severity"` // "error" | "warning"
	StepID      string `json:"step_id,omitempty"`
	Arg         string `json:"arg,omitempty"`
	Ref         string `json:"ref,omitempty"`
	Message     string `json:"message"`
	Remediation string `json:"remediation,omitempty"`
}

// ValidationReport is the full output of a validation pass.
type ValidationReport struct {
	OK       bool              `json:"ok"`
	Errors   []ValidationIssue `json:"errors"`
	Warnings []ValidationIssue `json:"warnings"`
}

// Validate type-checks every ${...} reference in the drawer against
// the upstream button output schemas and the drawer-level inputs. It
// also verifies structural invariants (step ids unique, no forward
// refs, button exists).
//
// Returns a report rather than a single error so the CLI can show an
// agent every issue at once — one round-trip per fix, not per bug.
func Validate(d *Drawer, btnSvc *button.Service) ValidationReport {
	report := ValidationReport{OK: true}

	// Build quick lookups.
	stepIDs := map[string]int{} // id -> index
	for i, st := range d.Steps {
		if st.ID == "" {
			report.Errors = append(report.Errors, ValidationIssue{
				Severity: "error", StepID: st.ID,
				Message:     fmt.Sprintf("step at position %d has no id", i),
				Remediation: "every step needs a unique id (defaults to the button name)",
			})
			continue
		}
		if _, exists := stepIDs[st.ID]; exists {
			report.Errors = append(report.Errors, ValidationIssue{
				Severity: "error", StepID: st.ID,
				Message:     fmt.Sprintf("duplicate step id %q", st.ID),
				Remediation: "rename one of the steps — step ids must be unique within a drawer",
			})
		}
		stepIDs[st.ID] = i
	}

	inputNames := map[string]InputDef{}
	for _, in := range d.Inputs {
		inputNames[in.Name] = in
	}

	// Cache button specs as we look them up.
	btnCache := map[string]*button.Button{}
	getBtn := func(name string) (*button.Button, error) {
		if b, ok := btnCache[name]; ok {
			return b, nil
		}
		b, err := btnSvc.Get(name)
		if err != nil {
			return nil, err
		}
		btnCache[name] = b
		return b, nil
	}

	drawerSvc := NewService()

	for i, st := range d.Steps {
		kind := st.Kind
		if kind == "" {
			kind = "button"
		}
		// kind=drawer: sub-drawer step. Verify the target exists and
		// that the step's args line up with the child's declared
		// inputs. Full ref resolution across drawer boundaries lives
		// in a future iteration of the CEL type checker; this pass
		// catches the common shape bugs.
		if kind == "drawer" {
			if st.Drawer == "" {
				report.Errors = append(report.Errors, ValidationIssue{
					Severity: "error", StepID: st.ID,
					Message:     "drawer-kind step has no drawer name",
					Remediation: "set step.drawer to an existing drawer's name",
				})
				continue
			}
			child, err := drawerSvc.Get(st.Drawer)
			if err != nil {
				report.Errors = append(report.Errors, ValidationIssue{
					Severity: "error", StepID: st.ID,
					Message:     fmt.Sprintf("sub-drawer %q not found", st.Drawer),
					Remediation: fmt.Sprintf("run `buttons drawer create %s` or fix the name", st.Drawer),
				})
				continue
			}
			// Required sub-drawer inputs should have a value from
			// step.args (literal or ref). Warn on missing so agents
			// see the connect hint without hard-failing.
			provided := map[string]bool{}
			for k := range st.Args {
				provided[k] = true
			}
			for _, in := range child.Inputs {
				if in.Required && !provided[in.Name] {
					report.Warnings = append(report.Warnings, ValidationIssue{
						Severity: "warning", StepID: st.ID, Arg: in.Name,
						Message:     fmt.Sprintf("sub-drawer %q requires input %q — not provided", st.Drawer, in.Name),
						Remediation: fmt.Sprintf("set `%s.args.%s` to a literal or ${ref}", st.ID, in.Name),
					})
				}
			}
			continue
		}
		if kind == "for_each" {
			if st.Over == "" {
				report.Errors = append(report.Errors, ValidationIssue{
					Severity: "error", StepID: st.ID,
					Message:     "for_each step has no 'over' expression",
					Remediation: "set step.over to a CEL expression that resolves to an array",
				})
			}
			if len(st.Steps) == 0 {
				report.Warnings = append(report.Warnings, ValidationIssue{
					Severity: "warning", StepID: st.ID,
					Message:     "for_each step has no nested steps — iteration will be a no-op",
					Remediation: "add at least one step inside step.steps",
				})
			}
			continue
		}
		if kind == "switch" {
			if len(st.Cases) == 0 && len(st.Steps) == 0 {
				report.Errors = append(report.Errors, ValidationIssue{
					Severity: "error", StepID: st.ID,
					Message:     "switch step has no cases and no default steps",
					Remediation: "add at least one case with a 'when' predicate, or default steps",
				})
			}
			for ci, c := range st.Cases {
				if c.When == "" {
					report.Errors = append(report.Errors, ValidationIssue{
						Severity: "error", StepID: st.ID,
						Message:     fmt.Sprintf("case %d has no 'when' predicate", ci),
						Remediation: "every case needs a when expression (CEL bool)",
					})
				}
			}
			continue
		}
		if kind == "aggregate" {
			if st.From == "" {
				report.Errors = append(report.Errors, ValidationIssue{
					Severity: "error", StepID: st.ID,
					Message:     "aggregate step has no 'from' expression",
					Remediation: "set step.from to a CEL expression producing an array",
				})
			}
			if st.Pluck == "" {
				report.Errors = append(report.Errors, ValidationIssue{
					Severity: "error", StepID: st.ID,
					Message:     "aggregate step has no 'pluck' expression",
					Remediation: "set step.pluck to a CEL expression; 'item' is the current entry",
				})
			}
			continue
		}
		if kind == "wait" {
			if st.Duration == "" && st.Until == "" {
				report.Errors = append(report.Errors, ValidationIssue{
					Severity: "error", StepID: st.ID,
					Message:     "wait step needs either 'duration' or 'until'",
					Remediation: "set step.duration='30s' or step.until='<RFC3339>'",
				})
			}
			if st.Duration != "" && st.Until != "" {
				report.Warnings = append(report.Warnings, ValidationIssue{
					Severity: "warning", StepID: st.ID,
					Message:     "wait step sets both 'duration' and 'until' — duration wins",
					Remediation: "keep only one",
				})
			}
			continue
		}
		if kind != "button" {
			// Future kinds are reserved but not runnable; the executor
			// errors with KIND_NOT_IMPLEMENTED. Warn so the validator
			// doesn't block authoring.
			report.Warnings = append(report.Warnings, ValidationIssue{
				Severity: "warning", StepID: st.ID,
				Message:     fmt.Sprintf("step kind %q is reserved but not executable yet", kind),
				Remediation: "only kind=button, kind=drawer, and kind=for_each execute today; this step will error at press time",
			})
			continue
		}

		if st.Button == "" {
			report.Errors = append(report.Errors, ValidationIssue{
				Severity: "error", StepID: st.ID,
				Message:     "button-kind step has no button name",
				Remediation: "set step.button to an existing button's name",
			})
			continue
		}

		btn, err := getBtn(st.Button)
		if err != nil {
			report.Errors = append(report.Errors, ValidationIssue{
				Severity: "error", StepID: st.ID,
				Message:     fmt.Sprintf("button %q not found", st.Button),
				Remediation: fmt.Sprintf("run `buttons create %s ...` or fix the button name on this step", st.Button),
			})
			continue
		}

		// For each arg, check every ${ref} inside. Also flag required
		// button args that have no value at all (neither literal nor
		// reference) — agents get a clear "missing arg" marker at
		// connect time rather than at press time.
		provided := map[string]bool{}
		for argName, argVal := range st.Args {
			provided[argName] = true
			refs := ExtractRefs(argVal)
			for _, ref := range refs {
				if issue := validateRef(ref, st.ID, argName, stepIDs, inputNames, d.Steps, i, getBtn); issue != nil {
					if issue.Severity == "error" {
						report.Errors = append(report.Errors, *issue)
					} else {
						report.Warnings = append(report.Warnings, *issue)
					}
				}
			}
			// Type compatibility: if the arg resolves to a whole-string
			// reference and we can infer the ref's type from upstream,
			// check it against the button's declared ArgDef type. This
			// is a stage-1 best-effort check; stage 2 with CEL's type
			// checker will be more thorough.
			if argDef := findArg(btn.Args, argName); argDef != nil {
				if issue := checkArgType(argVal, argDef, d.Steps, stepIDs, inputNames, getBtn); issue != nil {
					issue.StepID = st.ID
					issue.Arg = argName
					if issue.Severity == "error" {
						report.Errors = append(report.Errors, *issue)
					} else {
						report.Warnings = append(report.Warnings, *issue)
					}
				}
			}
		}

		// Flag missing required args.
		for _, a := range btn.Args {
			if a.Required && !provided[a.Name] {
				report.Warnings = append(report.Warnings, ValidationIssue{
					Severity: "warning", StepID: st.ID, Arg: a.Name,
					Message:     fmt.Sprintf("required arg %q not provided", a.Name),
					Remediation: fmt.Sprintf("connect from an upstream step, pass as drawer input, or set literal: `buttons drawer %s connect X.output.%s to %s.args.%s`", d.Name, a.Name, st.ID, a.Name),
				})
			}
		}
	}

	report.OK = len(report.Errors) == 0
	return report
}

// validateRef checks one ${path} reference. Returns an issue if the
// ref can't be resolved structurally (unknown root, forward ref,
// missing output field, etc). Does NOT do type-level checking here
// — that happens in checkArgType.
func validateRef(
	ref string,
	stepID, argName string,
	stepIDs map[string]int,
	inputs map[string]InputDef,
	steps []Step,
	currentIdx int,
	getBtn func(string) (*button.Button, error),
) *ValidationIssue {
	parts := strings.Split(ref, ".")
	if len(parts) == 0 {
		return &ValidationIssue{
			Severity: "error", StepID: stepID, Arg: argName, Ref: ref,
			Message: "empty reference path",
		}
	}

	root := parts[0]
	switch root {
	case "env":
		if len(parts) != 2 {
			return &ValidationIssue{
				Severity: "error", StepID: stepID, Arg: argName, Ref: ref,
				Message:     "env references must be ${env.VARNAME}",
				Remediation: "use ${env.MY_VAR} or $ENV{MY_VAR}",
			}
		}
		return nil

	case "inputs":
		if len(parts) < 2 {
			return &ValidationIssue{
				Severity: "error", StepID: stepID, Arg: argName, Ref: ref,
				Message: "input references must be ${inputs.<name>}",
			}
		}
		if _, ok := inputs[parts[1]]; !ok {
			return &ValidationIssue{
				Severity: "error", StepID: stepID, Arg: argName, Ref: ref,
				Message:     fmt.Sprintf("unknown drawer input %q", parts[1]),
				Remediation: "add the input to drawer.inputs or fix the reference name",
			}
		}
		return nil

	default:
		// Must be an upstream step id.
		idx, ok := stepIDs[root]
		if !ok {
			return &ValidationIssue{
				Severity: "error", StepID: stepID, Arg: argName, Ref: ref,
				Message:     fmt.Sprintf("unknown reference root %q (not an input, not a step id)", root),
				Remediation: "check for typos; valid roots: inputs.*, env.*, or an earlier step's id",
			}
		}
		if idx >= currentIdx {
			return &ValidationIssue{
				Severity: "error", StepID: stepID, Arg: argName, Ref: ref,
				Message:     fmt.Sprintf("forward reference: step %q runs at or after the current step", root),
				Remediation: "reorder steps so the referenced step runs first",
			}
		}

		// For <step>.output.<field>, try to check field exists in the
		// upstream button's output_schema (if declared).
		if len(parts) >= 3 && parts[1] == "output" {
			upstream := steps[idx]
			if upstream.Kind != "" && upstream.Kind != "button" {
				return nil // reserved kinds — skip schema check
			}
			btn, err := getBtn(upstream.Button)
			if err != nil || len(btn.OutputSchema) == 0 {
				// No schema declared — we can't verify, but we don't
				// block. Warn so the agent knows coverage is partial.
				return &ValidationIssue{
					Severity: "warning", StepID: stepID, Arg: argName, Ref: ref,
					Message:     fmt.Sprintf("upstream button %q has no output_schema — cannot verify field %q", upstream.Button, strings.Join(parts[2:], ".")),
					Remediation: fmt.Sprintf("add output_schema to button %q so references are type-checked", upstream.Button),
				}
			}
			if !schemaHasPath(btn.OutputSchema, parts[2:]) {
				return &ValidationIssue{
					Severity: "error", StepID: stepID, Arg: argName, Ref: ref,
					Message:     fmt.Sprintf("field %q not present in output_schema of button %q", strings.Join(parts[2:], "."), upstream.Button),
					Remediation: "fix the reference or update the button's output_schema",
				}
			}
		}
		return nil
	}
}

// checkArgType verifies that whole-string refs roughly match the
// downstream arg's type. Loose by design — JSON Schema types are a
// superset of ArgDef types, so we only flag mismatches when the
// upstream is clearly incompatible.
func checkArgType(
	argVal any,
	argDef *button.ArgDef,
	steps []Step,
	stepIDs map[string]int,
	inputs map[string]InputDef,
	getBtn func(string) (*button.Button, error),
) *ValidationIssue {
	s, ok := argVal.(string)
	if !ok {
		return nil // literals are validated at press time by ParsePressArgs
	}
	if !(strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") && strings.Count(s, "${") == 1) {
		return nil // mixed literals always resolve to strings; no extra check
	}
	path := s[2 : len(s)-1]
	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return nil
	}

	switch parts[0] {
	case "inputs":
		if len(parts) < 2 {
			return nil
		}
		in, ok := inputs[parts[1]]
		if !ok {
			return nil
		}
		if !typesCompatible(in.Type, argDef.Type) {
			return &ValidationIssue{
				Severity:    "error",
				Ref:         path,
				Message:     fmt.Sprintf("type mismatch: input %q is %q but arg expects %q", in.Name, in.Type, argDef.Type),
				Remediation: "change the input type, pick a compatible input, or coerce with an expression (stage 2)",
			}
		}
	default:
		if len(parts) < 3 || parts[1] != "output" {
			return nil
		}
		idx, ok := stepIDs[parts[0]]
		if !ok {
			return nil
		}
		upstream := steps[idx]
		if upstream.Kind != "" && upstream.Kind != "button" {
			return nil
		}
		btn, err := getBtn(upstream.Button)
		if err != nil || len(btn.OutputSchema) == 0 {
			return nil
		}
		schemaType, _ := schemaTypeAt(btn.OutputSchema, parts[2:])
		if schemaType != "" && !typesCompatibleSchema(schemaType, argDef.Type) {
			return &ValidationIssue{
				Severity:    "error",
				Ref:         path,
				Message:     fmt.Sprintf("type mismatch: %s is %q but arg expects %q", path, schemaType, argDef.Type),
				Remediation: "pick a compatible field or coerce with an expression (stage 2 CEL)",
			}
		}
	}
	return nil
}

// typesCompatible checks compatibility between two ArgDef type names.
func typesCompatible(src, dst string) bool {
	if src == dst {
		return true
	}
	// enum values are strings at the wire level.
	if src == "enum" && dst == "string" {
		return true
	}
	return false
}

// typesCompatibleSchema checks compatibility between a JSON Schema
// type and a button.ArgDef type name.
func typesCompatibleSchema(schemaType, argType string) bool {
	switch argType {
	case "string":
		return schemaType == "string"
	case "int":
		return schemaType == "integer" || schemaType == "number"
	case "bool":
		return schemaType == "boolean"
	case "enum":
		return schemaType == "string"
	}
	return true
}

// schemaHasPath walks a JSON Schema document looking for a field
// path. Returns true if the path is declared. Doesn't handle
// additionalProperties or pattern properties — stage 2 will upgrade.
func schemaHasPath(raw json.RawMessage, path []string) bool {
	if len(path) == 0 {
		return true
	}
	schema := map[string]any{}
	if err := json.Unmarshal(raw, &schema); err != nil {
		return false
	}
	return walkSchemaPath(schema, path)
}

func schemaTypeAt(raw json.RawMessage, path []string) (string, bool) {
	schema := map[string]any{}
	if err := json.Unmarshal(raw, &schema); err != nil {
		return "", false
	}
	cur := schema
	for _, seg := range path {
		props, ok := cur["properties"].(map[string]any)
		if !ok {
			return "", false
		}
		next, ok := props[seg].(map[string]any)
		if !ok {
			return "", false
		}
		cur = next
	}
	t, ok := cur["type"].(string)
	return t, ok
}

func walkSchemaPath(schema map[string]any, path []string) bool {
	cur := schema
	for _, seg := range path {
		props, ok := cur["properties"].(map[string]any)
		if !ok {
			return false
		}
		next, ok := props[seg].(map[string]any)
		if !ok {
			return false
		}
		cur = next
	}
	return true
}

func findArg(args []button.ArgDef, name string) *button.ArgDef {
	for i, a := range args {
		if a.Name == name {
			return &args[i]
		}
	}
	return nil
}
