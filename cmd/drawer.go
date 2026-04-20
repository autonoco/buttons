package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/drawer"
)

// drawer uses NAME-before-verb syntax for per-drawer operations:
//
//	buttons drawer create NAME                         (verb-first: creates a new drawer)
//	buttons drawer list                                (verb-first: lists all drawers)
//	buttons drawer NAME add BUTTON [BUTTON...]         (name-first: append buttons)
//	buttons drawer NAME connect BUTTON to BUTTON       (auto-match output→args)
//	buttons drawer NAME connect A.output.x to B.args.y (explicit field path)
//	buttons drawer NAME press [key=value ...]          (run the drawer)
//	buttons drawer NAME remove                         (delete this drawer)
//	buttons drawer NAME (no verb)                      (per-drawer summary)
//	buttons drawer schema                              (print embedded JSON Schema)
//
// The NAME-first shape reads as English for humans composing in their
// head ("for drawer X, connect A to B"). Agents don't care about word
// order; both shapes are equally parseable when `--json` is on.
//
// We intentionally don't register Cobra child commands — everything
// dispatches through drawerCmd.RunE so the NAME/VERB split works
// uniformly without fighting Cobra's subcommand matcher.
var drawerCmd = &cobra.Command{
	Use:   "drawer",
	Short: "Manage drawer workflows (chains of buttons)",
	Long: `Manage drawers — typed workflows that chain buttons with
${ref} references between steps.

Usage:
  buttons drawer create NAME
  buttons drawer list
  buttons drawer NAME add BUTTON [BUTTON...]
  buttons drawer NAME connect A to B
  buttons drawer NAME connect A.output.x to B.args.y
  buttons drawer NAME press [key=value ...]
  buttons drawer NAME remove
  buttons drawer NAME                  (show drawer summary)
  buttons drawer schema                (print JSON Schema)`,
	Args:          cobra.ArbitraryArgs,
	SilenceErrors: true,
	SilenceUsage:  true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}
		first := args[0]
		rest := args[1:]

		switch first {
		case "create":
			return drawerCreate(rest)
		case "list", "ls":
			return drawerList()
		case "schema":
			return drawerSchema()
		case "remove", "rm":
			if len(rest) != 1 {
				return fmt.Errorf("usage: buttons drawer remove NAME")
			}
			return drawerRemove(rest[0])
		}

		// first is a drawer name.
		name := first
		if len(rest) == 0 {
			// Bare `buttons drawer NAME` — show summary.
			return drawerShowSummary(name)
		}
		verb := rest[0]
		vargs := rest[1:]

		// --summary on any per-drawer mutation turns it into a
		// read-only plan: show the intended effect without applying
		// it. The subcommand dispatch is the right place for this
		// because every mutation shares the same rule.
		if SummaryRequested() && verb != "summary" {
			return drawerDryRun(name, verb, vargs)
		}

		switch verb {
		case "add":
			return drawerAdd(name, vargs)
		case "connect":
			return drawerConnect(name, vargs)
		case "press":
			return drawerPress(name, vargs)
		case "remove", "rm":
			return drawerRemove(name)
		case "summary":
			return drawerShowSummary(name)
		case "logs":
			return drawerLogs(name, vargs)
		default:
			return drawerUnknownVerb(name, verb)
		}
	},
}

func init() {
	// Register the logs flags on drawerCmd as well so
	// `buttons drawer NAME logs --failed --limit 50` parses. The
	// flag variables are package-level (declared in cmd/logs.go)
	// so the drawerLogs handler sees the right values regardless
	// of which command registered them.
	drawerCmd.Flags().BoolVar(&logsFailed, "failed", false, "only return runs that failed (for `NAME logs`)")
	drawerCmd.Flags().IntVar(&logsLimit, "limit", 20, "max runs to return (for `NAME logs`)")
	drawerCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "stream live progress (for `NAME logs`)")
	rootCmd.AddCommand(drawerCmd)
}

// ----------------- verb implementations -----------------

func drawerCreate(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: buttons drawer create NAME")
	}
	name := args[0]
	svc := drawer.NewService()
	d, err := svc.Create(name, "", nil)
	if err != nil {
		return handleDrawerError(err)
	}
	if jsonOutput {
		return config.WriteJSON(d)
	}
	fmt.Fprintf(os.Stderr, "Created drawer: %s\n", d.Name)
	printNextHint("buttons drawer %s add BUTTON [BUTTON...]", d.Name)
	return nil
}

func drawerList() error {
	svc := drawer.NewService()
	drawers, err := svc.List()
	if err != nil {
		return handleDrawerError(err)
	}
	if jsonOutput {
		return config.WriteJSON(drawers)
	}
	if len(drawers) == 0 {
		fmt.Fprintln(os.Stderr, "No drawers found. Create one with: buttons drawer create <name>")
		return nil
	}
	for _, d := range drawers {
		stepNames := make([]string, 0, len(d.Steps))
		for _, s := range d.Steps {
			stepNames = append(stepNames, s.ID)
		}
		desc := d.Description
		if desc == "" {
			desc = "-"
		}
		fmt.Printf("%s\t%s\t%s\n", d.Name, desc, strings.Join(stepNames, " → "))
	}
	return nil
}

func drawerAdd(name string, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: buttons drawer %s add BUTTON [BUTTON...]", name)
	}
	svc := drawer.NewService()
	d, err := svc.AddSteps(name, args)
	if err != nil {
		return handleDrawerError(err)
	}
	if jsonOutput {
		return config.WriteJSON(d)
	}
	fmt.Fprintf(os.Stderr, "Added %d step(s) to drawer %s\n", len(args), name)
	return nil
}

// drawerConnect handles both auto-match form and explicit form.
//
// Auto-match (BUTTON to BUTTON):
//
//	buttons drawer NAME connect apify-run to snowflake-insert
//
// Explicit (path=path):
//
//	buttons drawer NAME connect apify-run.output.body to snowflake-insert.args.rows
func drawerConnect(name string, args []string) error {
	// Normalize: accept both `A to B` and `A.output.x to B.args.y`.
	if len(args) != 3 || !strings.EqualFold(args[1], "to") {
		return fmt.Errorf("usage: buttons drawer %s connect FROM to TO", name)
	}
	from, to := args[0], args[2]

	fromParts := strings.SplitN(from, ".", 2)
	toParts := strings.SplitN(to, ".", 2)

	// Auto-match form: both sides are bare step/button ids.
	if len(fromParts) == 1 && len(toParts) == 1 {
		return drawerAutoConnect(name, fromParts[0], toParts[0])
	}

	// Explicit form: both sides must be <id>.output.<field> or
	// <id>.args.<field>. We require the source to reference
	// .output.<field> and the destination to reference
	// .args.<field> — this removes ambiguity.
	if !strings.Contains(from, ".output.") {
		return fmt.Errorf("source must be <step>.output.<field>, got %q", from)
	}
	if !strings.Contains(to, ".args.") {
		return fmt.Errorf("destination must be <step>.args.<field>, got %q", to)
	}
	return drawerExplicitConnect(name, from, to)
}

// drawerAutoConnect matches output fields from the source step to
// args on the destination step by name + compatible type. Returns
// AMBIGUOUS on multi-candidate matches so the agent retries with
// explicit paths.
func drawerAutoConnect(name, fromID, toID string) error {
	dsvc := drawer.NewService()
	bsvc := button.NewService()

	d, err := dsvc.Get(name)
	if err != nil {
		return handleDrawerError(err)
	}

	fromStep, toStep := findStepByID(d.Steps, fromID), findStepByID(d.Steps, toID)
	if fromStep == nil {
		return fmt.Errorf("step %q not found in drawer %q", fromID, name)
	}
	if toStep == nil {
		return fmt.Errorf("step %q not found in drawer %q", toID, name)
	}
	fromBtn, err := bsvc.Get(fromStep.Button)
	if err != nil {
		return err
	}
	toBtn, err := bsvc.Get(toStep.Button)
	if err != nil {
		return err
	}

	// Walk the destination button's required args; for each, find
	// candidate output fields on the source button by name + type.
	outputFields, err := topLevelFields(fromBtn.OutputSchema)
	if err != nil {
		return fmt.Errorf("source button %q has no output_schema — cannot auto-connect; use explicit form: A.output.field to B.args.field", fromBtn.Name)
	}

	type pair struct{ from, to string }
	var wired []pair
	var ambiguous []map[string]any
	for _, a := range toBtn.Args {
		if _, already := toStep.Args[a.Name]; already {
			continue // user already set this
		}
		candidates := []string{}
		for fname, ftype := range outputFields {
			if namesLooselyMatch(fname, a.Name) && jsonToArgTypeCompatible(ftype, a.Type) {
				candidates = append(candidates, fname)
			}
		}
		if len(candidates) == 0 {
			continue
		}
		if len(candidates) > 1 {
			ambiguous = append(ambiguous, map[string]any{
				"arg":        a.Name,
				"candidates": candidates,
				"from":       fromBtn.Name,
				"to":         toBtn.Name,
			})
			continue
		}
		wired = append(wired, pair{from: candidates[0], to: a.Name})
	}

	if len(ambiguous) > 0 {
		if jsonOutput {
			_ = config.WriteJSONError("AMBIGUOUS", "multiple candidate wirings; pick explicit form")
			fmt.Fprintf(os.Stderr, "%s\n", mustJSON(map[string]any{"ok": false, "error": map[string]any{"code": "AMBIGUOUS", "candidates": ambiguous, "remediation": fmt.Sprintf("use: buttons drawer %s connect %s.output.<field> to %s.args.<field>", name, fromID, toID)}}))
			return errSilent
		}
		return fmt.Errorf("ambiguous wiring: %d arg(s) had multiple candidates; use explicit form", len(ambiguous))
	}

	if len(wired) == 0 {
		return fmt.Errorf("no compatible fields to wire between %s and %s", fromBtn.Name, toBtn.Name)
	}

	// Persist the wiring.
	for _, p := range wired {
		ref := fmt.Sprintf("${%s.output.%s}", fromID, p.from)
		if _, err := dsvc.SetArg(name, toID, p.to, ref); err != nil {
			return handleDrawerError(err)
		}
	}

	if jsonOutput {
		return config.WriteJSON(map[string]any{"ok": true, "wired": wired})
	}
	for _, p := range wired {
		fmt.Fprintf(os.Stderr, "connected %s.output.%s → %s.args.%s\n", fromID, p.from, toID, p.to)
	}
	return nil
}

// drawerExplicitConnect writes a single user-specified connection.
func drawerExplicitConnect(name, from, to string) error {
	// from = <stepID>.output.<field>
	// to   = <stepID>.args.<field>
	outIdx := strings.Index(from, ".output.")
	fromID := from[:outIdx]
	fromField := from[outIdx+len(".output."):]

	argIdx := strings.Index(to, ".args.")
	toID := to[:argIdx]
	toField := to[argIdx+len(".args."):]

	ref := fmt.Sprintf("${%s.output.%s}", fromID, fromField)
	svc := drawer.NewService()
	if _, err := svc.SetArg(name, toID, toField, ref); err != nil {
		return handleDrawerError(err)
	}
	if jsonOutput {
		return config.WriteJSON(map[string]any{"ok": true, "wired": []map[string]string{{"from": fromField, "to": toField}}})
	}
	fmt.Fprintf(os.Stderr, "connected %s.output.%s → %s.args.%s\n", fromID, fromField, toID, toField)
	return nil
}

// drawerPress executes the drawer. Args after the verb are
// key=value pairs that fill in drawer-level inputs.
func drawerPress(name string, args []string) error {
	dsvc := drawer.NewService()
	d, err := dsvc.Get(name)
	if err != nil {
		return handleDrawerError(err)
	}

	inputValues, err := parseKV(args)
	if err != nil {
		return err
	}

	exec := drawer.NewExecutor()
	result, err := exec.Execute(context.Background(), d, inputValues)
	if err != nil {
		return err
	}
	if jsonOutput {
		return config.WriteJSON(result)
	}
	if result.Status == "ok" {
		fmt.Fprintf(os.Stderr, "✓ drawer %s ok (%dms)\n", name, result.DurationMs)
	} else {
		fmt.Fprintf(os.Stderr, "✗ drawer %s failed at step %s: %s\n", name, result.FailedStep, func() string {
			if result.Error != nil {
				return result.Error.Message
			}
			return "unknown"
		}())
	}
	return nil
}

func drawerRemove(name string) error {
	svc := drawer.NewService()
	if err := svc.Remove(name); err != nil {
		return handleDrawerError(err)
	}
	if jsonOutput {
		return config.WriteJSON(map[string]any{"ok": true, "removed": name})
	}
	fmt.Fprintf(os.Stderr, "Removed drawer: %s\n", name)
	return nil
}

func drawerSchema() error {
	// Dump the embedded drawer JSON Schema. Use stdout so `buttons
	// drawer schema | jq .` works cleanly.
	_, err := os.Stdout.Write(drawer.SchemaJSON)
	return err
}

// drawerLogs returns past runs for a specific drawer. Shares the
// same flags as `buttons NAME logs` (--failed, --limit). The TUI
// live-follow mode isn't wired for drawers in stage 2 — drawers
// are sequential orchestration and per-button `buttons NAME logs
// --follow` covers the live-press case at the step level.
func drawerLogs(name string, vargs []string) error {
	n := logsLimit
	if n <= 0 {
		n = 20
	}
	runs, err := drawer.ListRuns(name, n)
	if err != nil {
		return handleDrawerError(err)
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
		fmt.Fprintf(os.Stderr, "no runs for drawer %s yet\n", name)
		return nil
	}
	for _, r := range runs {
		status := r.Status
		if r.ErrorType != "" {
			status = r.Status + " · " + r.ErrorType
		}
		fmt.Printf("%s  %s  %dms\n", r.StartedAt.Local().Format("2006-01-02 15:04:05"), status, r.DurationMs)
	}
	// vargs would be used if we later added step-filters; silence
	// the unused warning without losing the parameter.
	_ = vargs
	return nil
}

// drawerShowSummary prints the drawer introspection view — topology,
// inputs, recent runs, validation state. Used by bare
// `buttons drawer NAME` and by the `--summary` flag on mutations.
func drawerShowSummary(name string) error {
	dsvc := drawer.NewService()
	d, err := dsvc.Get(name)
	if err != nil {
		return handleDrawerError(err)
	}

	bsvc := button.NewService()
	report := drawer.Validate(d, bsvc)

	runs, _ := drawer.ListRuns(name, 3)

	topology := make([]string, 0, len(d.Steps))
	for _, s := range d.Steps {
		topology = append(topology, s.ID)
	}

	snapshot := map[string]any{
		"name":        d.Name,
		"description": d.Description,
		"inputs":      d.Inputs,
		"steps":       d.Steps,
		"topology":    strings.Join(topology, " → "),
		"validation":  report,
		"recent_runs": summarizeDrawerRuns(runs),
	}

	if jsonOutput {
		return config.WriteJSON(snapshot)
	}
	fmt.Printf("drawer %s\n", d.Name)
	if d.Description != "" {
		fmt.Printf("  %s\n", d.Description)
	}
	fmt.Printf("  %s\n", strings.Join(topology, " → "))
	if !report.OK {
		fmt.Printf("  validation: %d error(s), %d warning(s)\n", len(report.Errors), len(report.Warnings))
		for _, e := range report.Errors {
			fmt.Printf("    ✗ %s: %s\n", e.StepID, e.Message)
			if e.Remediation != "" {
				fmt.Printf("      → %s\n", e.Remediation)
			}
		}
	} else {
		fmt.Printf("  validation: ok\n")
	}
	if len(runs) > 0 {
		fmt.Printf("  recent runs:\n")
		for _, r := range runs {
			fmt.Printf("    %s  %s  %dms\n", r.StartedAt.Local().Format("2006-01-02 15:04"), r.Status, r.DurationMs)
		}
	}
	return nil
}

// drawerDryRun produces the read-only plan for a per-drawer verb
// without mutating disk. Surfaces what the mutation WOULD do so the
// agent can review before committing.
func drawerDryRun(name, verb string, vargs []string) error {
	dsvc := drawer.NewService()
	d, err := dsvc.Get(name)
	if err != nil {
		return handleDrawerError(err)
	}

	plan := map[string]any{
		"drawer": name,
		"verb":   verb,
		"args":   vargs,
	}

	switch verb {
	case "press":
		inputValues, perr := parseKV(vargs)
		if perr != nil {
			return perr
		}
		plan["inputs_resolved"] = inputValues
		plan["steps_to_run"] = stepPlanFor(d, inputValues)
		bsvc := button.NewService()
		plan["validation"] = drawer.Validate(d, bsvc)
	case "add":
		plan["would_append"] = vargs
	case "connect":
		plan["would_connect"] = vargs
	case "remove", "rm":
		plan["would_remove"] = name
	}
	plan["summary"] = true
	plan["executed"] = false

	if jsonOutput {
		return config.WriteJSON(plan)
	}
	fmt.Fprintf(os.Stderr, "DRY RUN — %s %s\n", name, verb)
	fmt.Fprintln(os.Stderr, mustJSON(plan))
	return nil
}

// stepPlanFor walks the drawer and returns each step's resolved args
// (or the reason a ref could not resolve). Used by the press dry-run.
func stepPlanFor(d *drawer.Drawer, inputs map[string]any) []map[string]any {
	ctx := drawer.Context{"inputs": inputs}
	out := make([]map[string]any, 0, len(d.Steps))
	for _, s := range d.Steps {
		entry := map[string]any{
			"id":     s.ID,
			"kind":   firstNonEmptyStr(s.Kind, "button"),
			"button": s.Button,
		}
		resolved := map[string]any{}
		for k, v := range s.Args {
			r, err := drawer.Resolve(v, ctx)
			if err != nil {
				resolved[k] = map[string]any{"unresolved": fmt.Sprintf("%v", err)}
			} else {
				resolved[k] = r
			}
		}
		entry["args_resolved"] = resolved
		out = append(out, entry)
		// Seed downstream refs with a placeholder so later steps can
		// at least show structure (real output isn't known dry-run).
		ctx[s.ID] = map[string]any{"output": map[string]any{}}
	}
	return out
}

func firstNonEmptyStr(xs ...string) string {
	for _, s := range xs {
		if s != "" {
			return s
		}
	}
	return ""
}

// ----------------- helpers -----------------

func drawerUnknownVerb(name, verb string) error {
	msg := fmt.Sprintf("unknown drawer verb %q (use: add, connect, press, remove, summary)", verb)
	if jsonOutput {
		_ = config.WriteJSONError("UNKNOWN_VERB", msg)
		return errSilent
	}
	_ = name
	return fmt.Errorf("%s", msg)
}

func handleDrawerError(err error) error {
	var dse *drawer.ServiceError
	if errors.As(err, &dse) {
		if jsonOutput {
			_ = config.WriteJSONError(dse.Code, dse.Message)
			return errSilent
		}
		return fmt.Errorf("%s: %s", dse.Code, dse.Message)
	}
	// button service errors bubble up from AddSteps when a referenced
	// button doesn't exist; reuse the button handler.
	return handleServiceError(err)
}

func parseKV(args []string) (map[string]any, error) {
	out := map[string]any{}
	for _, raw := range args {
		idx := strings.Index(raw, "=")
		if idx < 0 {
			return nil, fmt.Errorf("expected key=value, got %q", raw)
		}
		k, v := raw[:idx], raw[idx+1:]
		// Try to parse as JSON literal (number, bool, object, array)
		// — falls through to string if that fails. Lets agents pass
		// structured values without explicit typing flags.
		var parsed any
		if err := json.Unmarshal([]byte(v), &parsed); err == nil {
			out[k] = parsed
		} else {
			out[k] = v
		}
	}
	return out, nil
}

func findStepByID(steps []drawer.Step, id string) *drawer.Step {
	for i, s := range steps {
		if s.ID == id {
			return &steps[i]
		}
	}
	return nil
}

// topLevelFields returns the { name: jsonSchemaType } map of top-level
// properties in a JSON Schema document. Returns an error if the
// schema is absent or unparseable.
func topLevelFields(raw json.RawMessage) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("no schema")
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	props, ok := doc["properties"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("schema has no top-level properties")
	}
	out := map[string]string{}
	for k, v := range props {
		sub, ok := v.(map[string]any)
		if !ok {
			continue
		}
		t, _ := sub["type"].(string)
		out[k] = t
	}
	return out, nil
}

func namesLooselyMatch(a, b string) bool {
	return strings.EqualFold(a, b)
}

func jsonToArgTypeCompatible(jsonType, argType string) bool {
	switch argType {
	case "string":
		return jsonType == "string"
	case "int":
		return jsonType == "integer" || jsonType == "number"
	case "bool":
		return jsonType == "boolean"
	case "enum":
		return jsonType == "string"
	}
	return jsonType != ""
}

func mustJSON(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}

func summarizeDrawerRuns(runs []drawer.Run) []map[string]any {
	out := make([]map[string]any, 0, len(runs))
	for _, r := range runs {
		entry := map[string]any{
			"run_id":      r.RunID,
			"status":      r.Status,
			"duration_ms": r.DurationMs,
			"started_at":  r.StartedAt,
		}
		if r.ErrorType != "" {
			entry["error_type"] = r.ErrorType
		}
		out = append(out, entry)
	}
	return out
}
