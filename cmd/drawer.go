package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/drawer"
	"github.com/autonoco/buttons/internal/webhook"
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
  buttons drawer NAME add BUTTON [BUTTON ...]         append button step(s)
  buttons drawer NAME add drawer/OTHER                append a sub-drawer step
  buttons drawer NAME add for_each:BUTTON             append a per-item loop wrapping BUTTON
  buttons drawer NAME add wait:DURATION               append a time-based pause (e.g. wait:30s)
  buttons drawer NAME connect A to B                  auto-match output → args by name+type
  buttons drawer NAME connect A.output.x to B.args.y  explicit field path
  buttons drawer NAME set STEP.args.FIELD=value       literal or ${ref} into a step arg
  buttons drawer NAME set STEP.over=EXPR              for_each: the array to iterate
  buttons drawer NAME set STEP.as=NAME                for_each: loop variable name
  buttons drawer NAME set STEP.parallelism=N          for_each: max concurrent iterations (0/1 = serial)
  buttons drawer NAME set STEP.from=EXPR              aggregate: input array
  buttons drawer NAME set STEP.pluck=EXPR             aggregate: per-item expression
  buttons drawer NAME set STEP.steps.N.args.F=value   reach a nested step's arg
  buttons drawer NAME press [key=value ...]           run it; unfilled required inputs go here
  buttons drawer NAME logs [--failed] [--limit N]     past runs for this drawer
  buttons drawer NAME remove                          delete the drawer
  buttons drawer NAME                                 summary (topology + validation + recent runs)
  buttons drawer schema                               print JSON Schema for drawer.json

Typical authoring flow:
  buttons drawer create deploy-flow
  buttons drawer deploy-flow add build publish
  buttons drawer deploy-flow connect build to publish
  buttons drawer deploy-flow set publish.args.env=prod
  buttons drawer deploy-flow press`,
	Args:          cobra.ArbitraryArgs,
	SilenceErrors: true,
	SilenceUsage:  true,
	// Per-verb flags (e.g. `trigger webhook --auth basic --auth-user ...`)
	// are hand-parsed by drawerTrigger / drawerSet / drawerPress so the
	// surface varies per verb without registering every permutation on
	// drawerCmd. DisableFlagParsing gives us the raw args intact; we
	// still honor --json by scanning for it below (rootCmd's Persistent
	// PreRunE also auto-sets jsonOutput when stdout is non-TTY).
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Manual --json handling because DisableFlagParsing took the
		// usual cobra path out of the loop.
		args = extractJSONFlag(args)
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
		case "set":
			return drawerSet(name, vargs)
		case "trigger":
			return drawerTrigger(name, vargs)
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
	// drawerCmd has DisableFlagParsing = true so per-verb flags are
	// hand-parsed inside drawerLogs / drawerTrigger / drawerPress /
	// drawerSet. Nothing to register here.
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
		// Webhook-triggered drawers get a trailing tag so agents
		// scanning the list see which ones are reachable via the
		// listener. Quick-mode users see "webhook" with no URL; named
		// mode shows the full URL.
		wh := ""
		for _, t := range d.Triggers {
			if t.Kind == "webhook" && t.Path != "" {
				cfg, _ := webhook.LoadConfig()
				if cfg != nil && cfg.Mode == webhook.ModeNamed {
					wh = "\twebhook=https://" + cfg.Hostname + t.Path
				} else {
					wh = "\twebhook=" + t.Path
				}
				break
			}
		}
		fmt.Printf("%s\t%s\t%s%s\n", d.Name, desc, strings.Join(stepNames, " → "), wh)
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
	// Point at the next logical step. If there are 2+ steps now
	// (most drawers start with 0 and get 1 or more added at once),
	// suggest wiring them. Otherwise suggest adding more.
	if len(d.Steps) >= 2 {
		printNextHint("buttons drawer %s connect %s to %s",
			d.Name, d.Steps[len(d.Steps)-2].ID, d.Steps[len(d.Steps)-1].ID)
	} else {
		printNextHint("buttons drawer %s add MORE_BUTTONS", d.Name)
	}
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
	printNextHint("buttons drawer %s press", name)
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
	printNextHint("buttons drawer %s press", name)
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

	// Split --webhook-body <value> out of args before parseKV sees it.
	// Accepts three forms so agents can pick what's ergonomic:
	//   --webhook-body '{"foo":1}'            inline JSON literal
	//   --webhook-body @fixture.json          path to a JSON fixture
	//   --webhook-body=<form-above>           = form
	args, webhookBody, err := extractWebhookBody(args)
	if err != nil {
		return err
	}

	inputValues, err := parseKV(args)
	if err != nil {
		return err
	}

	// If --webhook-body was supplied, synthesize the same inputs.webhook
	// shape that `buttons webhook listen` produces. Lets agents test a
	// webhook-triggered drawer without starting the listener — iterate
	// locally, curl for integration.
	if webhookBody != nil {
		inputValues["webhook"] = map[string]any{
			"body":        webhookBody,
			"headers":     map[string]string{},
			"query":       map[string]string{},
			"method":      "POST",
			"path":        webhookTriggerPath(d),
			"received_at": time.Now().UTC().Format(time.RFC3339),
		}
	}

	exec := drawer.NewExecutor()
	result, err := exec.Execute(context.Background(), d, inputValues)
	if err != nil {
		return err
	}
	if jsonOutput {
		if result.Status == "ok" {
			return config.WriteJSON(result)
		}
		// Drawer failed — still emit the full result on stdout so
		// agents can parse the failure envelope, but exit non-zero
		// via errSilent so pipelines notice.
		_ = config.WriteJSON(result)
		return errSilent
	}
	if result.Status == "ok" {
		fmt.Fprintf(os.Stderr, "✓ drawer %s ok (%dms)\n", name, result.DurationMs)
		printNextHint("buttons drawer %s logs", name)
		return nil
	}
	fmt.Fprintf(os.Stderr, "✗ drawer %s failed at step %s: %s\n", name, result.FailedStep, func() string {
		if result.Error != nil {
			return result.Error.Message
		}
		return "unknown"
	}())
	printNextHint("buttons drawer %s logs --failed", name)
	return errSilent
}

// drawerSet writes literal values or ${ref} expressions into a
// step's args. Accepts dotted-path targets so agents can address
// both button-step args and sub-drawer-step args with one verb:
//
//	buttons drawer deploy set build.args.env=prod
//	buttons drawer deploy set publish.args.version='${build.output.version}'
//	buttons drawer deploy set child-flow.args.token='${env.APIFY_TOKEN}'
//
// Multiple pairs in one call are applied atomically enough — we
// load the drawer once, mutate each arg, and save once at the end.
// A parse error on any pair aborts the whole call before any
// write hits disk.
func drawerSet(name string, vargs []string) error {
	if len(vargs) < 1 {
		return fmt.Errorf("usage: buttons drawer %s set STEP.PATH=value [STEP.PATH=value ...]\n  paths: STEP.args.FIELD | STEP.over | STEP.as | STEP.from | STEP.pluck | STEP.steps.N.args.FIELD", name)
	}

	svc := drawer.NewService()
	applied := make([]string, 0, len(vargs))

	for _, raw := range vargs {
		eq := strings.Index(raw, "=")
		if eq < 0 {
			return fmt.Errorf("expected STEP.PATH=value, got %q", raw)
		}
		target, rawValue := raw[:eq], raw[eq+1:]
		// Literal JSON first (numbers/bools/objects); fall through
		// to string so ${ref} expressions and plain text stash as-is.
		var value any
		if err := json.Unmarshal([]byte(rawValue), &value); err != nil {
			value = rawValue
		}

		parts := strings.SplitN(target, ".", 2)
		if len(parts) < 2 {
			return fmt.Errorf("target must include a step id and a path, got %q", target)
		}
		stepID, path := parts[0], parts[1]
		if stepID == "" {
			return fmt.Errorf("empty step id in %q", target)
		}

		// Route by path shape:
		//   args.FIELD                 → SetArg on the outer step
		//   steps.N.args.FIELD         → SetNestedArg on for_each/switch body
		//   <field>                    → SetField (over, as, from, pluck, ...)
		switch {
		case strings.HasPrefix(path, "args."):
			field := strings.TrimPrefix(path, "args.")
			if field == "" {
				return fmt.Errorf("empty arg name in %q", target)
			}
			if _, err := svc.SetArg(name, stepID, field, value); err != nil {
				return handleDrawerError(err)
			}
		case strings.HasPrefix(path, "steps."):
			rest := strings.TrimPrefix(path, "steps.")
			i := strings.Index(rest, ".")
			if i <= 0 {
				return fmt.Errorf("nested path %q missing .args.FIELD suffix", target)
			}
			idxStr := rest[:i]
			rest = rest[i+1:]
			var nestedIdx int
			if _, err := fmt.Sscanf(idxStr, "%d", &nestedIdx); err != nil {
				return fmt.Errorf("expected numeric index in %q, got %q", target, idxStr)
			}
			if !strings.HasPrefix(rest, "args.") {
				return fmt.Errorf("nested path must end in .args.FIELD, got %q", target)
			}
			argName := strings.TrimPrefix(rest, "args.")
			if argName == "" {
				return fmt.Errorf("empty nested arg name in %q", target)
			}
			if _, err := svc.SetNestedArg(name, stepID, nestedIdx, argName, value); err != nil {
				return handleDrawerError(err)
			}
		default:
			// Bare field — over, as, from, pluck, button, drawer,
			// on_item_failure. Always a string on the wire.
			strVal, ok := value.(string)
			if !ok {
				strVal = rawValue
			}
			if _, err := svc.SetField(name, stepID, path, strVal); err != nil {
				return handleDrawerError(err)
			}
		}
		applied = append(applied, target)
	}

	if jsonOutput {
		return config.WriteJSON(map[string]any{
			"ok":     true,
			"drawer": name,
			"set":    len(applied),
			"paths":  applied,
		})
	}
	for _, t := range applied {
		fmt.Fprintf(os.Stderr, "set %s\n", t)
	}
	printNextHint("buttons drawer %s press", name)
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

// drawerTrigger handles `buttons drawer NAME trigger webhook [PATH] [auth-flags]`.
// Only kind=webhook is live today; other trigger kinds declared in the
// schema return NOT_IMPLEMENTED.
//
// Auth flags mirror n8n's four webhook auth types:
//
//	--auth none                                            (default)
//	--auth basic --auth-user <user> --auth-pass <pass>
//	--auth header --auth-header-name X-Foo --auth-header-value <val>
//	--auth jwt    --jwt-secret <secret>
//	                [--jwt-algorithm HS256|HS384|HS512]
//	                [--jwt-issuer <iss>] [--jwt-audience <aud>]
//
// Any *-value / *-secret / *-pass field accepts literal strings OR
// the form '$ENV{VAR_NAME}' which the listener resolves against its
// environment at match time — keeps committed drawer.json from
// carrying raw secrets.
func drawerTrigger(name string, vargs []string) error {
	if len(vargs) < 1 {
		return fmt.Errorf("usage: buttons drawer %s trigger webhook [PATH] [--auth <type> ...]\n  examples:\n    buttons drawer %s trigger webhook /apify\n    buttons drawer %s trigger webhook /gh --auth header --auth-header-name X-Hub-Signature --auth-header-value '$ENV{GH_SECRET}'\n    buttons drawer %s trigger webhook /stripe --auth jwt --jwt-secret '$ENV{STRIPE_JWT_KEY}' --jwt-issuer stripe.com", name, name, name, name)
	}
	kind := vargs[0]
	vargs = vargs[1:]

	if kind != "webhook" {
		return fmt.Errorf("trigger kind %q not implemented; only 'webhook' is available today", kind)
	}

	// Scalar-flag loop keeps drawerTrigger pflag-free. Accepts both
	// `--flag value` and `--flag=value` forms for every flag so agents
	// can use whichever they prefer.
	var path string
	auth := &drawer.TriggerAuth{Type: "none"}
	for i := 0; i < len(vargs); i++ {
		a := vargs[i]
		val := func() (string, error) {
			if i+1 >= len(vargs) {
				return "", fmt.Errorf("%s needs a value", a)
			}
			v := vargs[i+1]
			i++
			return v, nil
		}
		// =form handler factored out so the switch stays readable.
		if eq := strings.Index(a, "="); eq > 0 && strings.HasPrefix(a, "--") {
			key, v := a[:eq], a[eq+1:]
			if err := applyTriggerFlag(key, v, &path, auth); err != nil {
				return err
			}
			continue
		}
		switch a {
		case "--path", "--auth", "--auth-user", "--auth-pass",
			"--auth-header-name", "--auth-header-value",
			"--jwt-secret", "--jwt-algorithm", "--jwt-issuer", "--jwt-audience":
			v, err := val()
			if err != nil {
				return err
			}
			if err := applyTriggerFlag(a, v, &path, auth); err != nil {
				return err
			}
		default:
			if strings.HasPrefix(a, "-") {
				return fmt.Errorf("unknown flag %q", a)
			}
			if path == "" {
				path = a
			} else {
				return fmt.Errorf("unexpected extra positional %q", a)
			}
		}
	}
	if path == "" {
		// Default path: /<drawer-name>. Agents who don't care about URL
		// design get a sensible default; the verb still accepts an
		// explicit override.
		path = "/" + name
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	// Collapse the zero-value "none" auth back to a nil so we don't
	// write a useless block into drawer.json for the common case.
	if auth.Type == "" || auth.Type == "none" {
		auth = nil
	}

	svc := drawer.NewService()
	d, err := svc.SetWebhookTrigger(name, path, auth)
	if err != nil {
		return handleDrawerError(err)
	}

	// Compose the public URL if webhook setup has been run so agents
	// see the exact string they'd paste into the upstream service.
	cfg, _ := webhook.LoadConfig()
	var publicURL string
	if cfg != nil && cfg.Mode == webhook.ModeNamed {
		publicURL = "https://" + cfg.Hostname + path
	}

	inputShape := webhookInputShape()
	authType := triggerAuthType(auth)

	if jsonOutput {
		return config.WriteJSON(map[string]any{
			"drawer":      d.Name,
			"kind":        "webhook",
			"path":        path,
			"auth_type":   authType,
			"public_url":  publicURL,
			"input_shape": inputShape,
		})
	}
	fmt.Fprintf(os.Stderr, "Registered webhook trigger on %s\n", d.Name)
	fmt.Fprintf(os.Stderr, "  path:   %s\n", path)
	fmt.Fprintf(os.Stderr, "  auth:   %s\n", authType)
	if publicURL != "" {
		fmt.Fprintf(os.Stderr, "  url:    %s\n", publicURL)
	} else {
		fmt.Fprintf(os.Stderr, "  (run `buttons webhook setup` to establish a stable public URL)\n")
	}
	fmt.Fprintf(os.Stderr, "\nAvailable inputs inside this drawer's steps:\n")
	for _, line := range inputShape {
		fmt.Fprintf(os.Stderr, "  %s\n", line)
	}
	fmt.Fprintf(os.Stderr, "\nCross-drawer reference (from another drawer's step args):\n")
	fmt.Fprintf(os.Stderr, "  ${webhooks.%s}   — resolves to the full public URL above\n", d.Name)
	printNextHint("buttons webhook listen   # start the dispatcher")
	return nil
}

// extractJSONFlag scans args for --json / --json=true / --no-json and
// sets the package-level jsonOutput bool accordingly. Returns the
// residual args so the per-verb hand-parsers don't have to know about
// the flag. Also strips --help so `buttons drawer NAME --help`
// doesn't confuse our dispatch — Cobra's automatic help path is
// intentional for `buttons drawer --help` (no subcommand) and handled
// upstream by the RunE when args is empty.
func extractJSONFlag(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		switch a {
		case "--json", "--json=true":
			jsonOutput = true
		case "--json=false", "--no-json":
			jsonOutput = false
		default:
			out = append(out, a)
		}
	}
	return out
}

// applyTriggerFlag routes a parsed --key=value onto the right field
// of the TriggerAuth block (or onto path). Factored out so both the
// `--flag value` and `--flag=value` paths share one code path.
func applyTriggerFlag(key, value string, path *string, auth *drawer.TriggerAuth) error {
	switch key {
	case "--path":
		*path = value
	case "--auth":
		switch value {
		case "none", "basic", "header", "jwt":
			auth.Type = value
		default:
			return fmt.Errorf("--auth must be one of none|basic|header|jwt (got %q)", value)
		}
	case "--auth-user":
		auth.Username = value
	case "--auth-pass":
		auth.Password = value
	case "--auth-header-name":
		auth.HeaderName = value
	case "--auth-header-value":
		auth.HeaderValue = value
	case "--jwt-secret":
		auth.JWTSecret = value
	case "--jwt-algorithm":
		auth.JWTAlgorithm = value
	case "--jwt-issuer":
		auth.JWTIssuer = value
	case "--jwt-audience":
		auth.JWTAudience = value
	default:
		return fmt.Errorf("unknown flag %q", key)
	}
	return nil
}

// extractWebhookBody scans args for the --webhook-body flag, pulls it
// out, and parses the value into a JSON-ish shape. Returns the residual
// args (for parseKV) and the parsed body (nil when flag absent). Three
// forms accepted: --webhook-body '<json>', --webhook-body @file, and
// --webhook-body=<either>.
func extractWebhookBody(args []string) ([]string, any, error) {
	rest := make([]string, 0, len(args))
	var rawVal string
	var seen bool
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--webhook-body":
			if i+1 >= len(args) {
				return nil, nil, fmt.Errorf("--webhook-body needs a value (inline JSON or @path)")
			}
			rawVal = args[i+1]
			seen = true
			i++
		case strings.HasPrefix(a, "--webhook-body="):
			rawVal = strings.TrimPrefix(a, "--webhook-body=")
			seen = true
		default:
			rest = append(rest, a)
		}
	}
	if !seen {
		return rest, nil, nil
	}
	// @path form — read the file and parse.
	if strings.HasPrefix(rawVal, "@") {
		path := strings.TrimPrefix(rawVal, "@")
		data, err := os.ReadFile(path) // #nosec G304 -- user-supplied path for dry-run fixture
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", path, err)
		}
		rawVal = string(data)
	}
	var parsed any
	if strings.TrimSpace(rawVal) == "" {
		parsed = nil
	} else if err := json.Unmarshal([]byte(rawVal), &parsed); err != nil {
		// Not JSON — pass through as a string so the drawer can
		// still observe the value.
		parsed = rawVal
	}
	return rest, parsed, nil
}

// webhookTriggerPath returns the drawer's registered webhook path so
// the dry-run ${inputs.webhook.path} matches what a real POST would
// produce. Falls back to "/" + drawer name when no trigger is set (the
// same default as `drawer NAME trigger webhook`).
func webhookTriggerPath(d *drawer.Drawer) string {
	for _, t := range d.Triggers {
		if t.Kind == "webhook" && t.Path != "" {
			return t.Path
		}
	}
	return "/" + d.Name
}

// webhookInputShape lists the ${inputs.webhook.*} shape an incoming
// webhook materializes inside a triggered drawer. Kept as data (not a
// prose blob) so `--json` callers get a structured enumeration.
//
// Keep this in sync with serveHandler.ServeHTTP in cmd/serve.go — the
// fields it writes into the webhookInput map must show up here.
func webhookInputShape() []string {
	return []string{
		"${inputs.webhook.body}              — parsed JSON body (object | array | string | null)",
		"${inputs.webhook.body.<field>}      — drill into the parsed body",
		"${inputs.webhook.headers.<Header>}  — single-value request headers (e.g. X-Signature)",
		"${inputs.webhook.query.<param>}     — query string params",
		"${inputs.webhook.method}            — POST",
		"${inputs.webhook.path}              — the trigger path (e.g. /apify)",
		"${inputs.webhook.received_at}       — RFC3339 UTC timestamp",
	}
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
	// DisableFlagParsing on drawerCmd means cobra no longer fills the
	// logsLimit/logsFailed/logsFollow globals; parse them here from
	// vargs instead. Keeps the verb's surface unchanged.
	failed := false
	limit := 20
	for i := 0; i < len(vargs); i++ {
		a := vargs[i]
		switch {
		case a == "--failed":
			failed = true
		case a == "--limit":
			if i+1 >= len(vargs) {
				return fmt.Errorf("--limit needs a value")
			}
			if _, err := fmt.Sscanf(vargs[i+1], "%d", &limit); err != nil {
				return fmt.Errorf("--limit needs an integer, got %q", vargs[i+1])
			}
			i++
		case strings.HasPrefix(a, "--limit="):
			if _, err := fmt.Sscanf(strings.TrimPrefix(a, "--limit="), "%d", &limit); err != nil {
				return fmt.Errorf("--limit needs an integer, got %q", a)
			}
		case a == "-f", a == "--follow":
			// Follow mode isn't implemented for drawers at the orchestrator
			// level (see function comment). Accept the flag silently so
			// agents that pass it get no surprise error; point them at
			// button-level follow.
			_ = a
		default:
			return fmt.Errorf("unknown flag %q", a)
		}
	}
	if limit <= 0 {
		limit = 20
	}
	runs, err := drawer.ListRuns(name, limit)
	if err != nil {
		return handleDrawerError(err)
	}
	if failed {
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

	// Webhook trigger surfacing: if this drawer has one, compute the
	// full public URL so agents see exactly what to paste into the
	// upstream service.
	triggersOut := summarizeDrawerTriggers(d)

	snapshot := map[string]any{
		"name":        d.Name,
		"description": d.Description,
		"inputs":      d.Inputs,
		"steps":       d.Steps,
		"topology":    strings.Join(topology, " → "),
		"validation":  report,
		"recent_runs": summarizeDrawerRuns(runs),
		"triggers":    triggersOut,
	}

	if jsonOutput {
		return config.WriteJSON(snapshot)
	}
	fmt.Printf("drawer %s\n", d.Name)
	if d.Description != "" {
		fmt.Printf("  %s\n", d.Description)
	}
	fmt.Printf("  %s\n", strings.Join(topology, " → "))
	for _, tg := range triggersOut {
		if tg["kind"] == "webhook" {
			url, _ := tg["url"].(string)
			path, _ := tg["path"].(string)
			if url != "" {
				fmt.Printf("  webhook:    %s\n", url)
			} else {
				fmt.Printf("  webhook:    %s  (no public URL — run `buttons webhook setup`)\n", path)
			}
		}
	}
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

// summarizeDrawerTriggers projects a drawer's declared triggers into
// summary JSON, computing the public URL for each webhook trigger when
// the named tunnel is configured. Quick-mode users see the path only;
// the URL surfaces after `buttons webhook setup`.
func summarizeDrawerTriggers(d *drawer.Drawer) []map[string]any {
	cfg, _ := webhook.LoadConfig()
	var host string
	if cfg != nil && cfg.Mode == webhook.ModeNamed {
		host = cfg.Hostname
	}
	out := make([]map[string]any, 0, len(d.Triggers))
	for _, t := range d.Triggers {
		entry := map[string]any{
			"kind":      t.Kind,
			"path":      t.Path,
			"auth_type": triggerAuthType(t.Auth),
		}
		if t.Kind == "webhook" && host != "" {
			entry["url"] = "https://" + host + t.Path
		}
		out = append(out, entry)
	}
	return out
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
