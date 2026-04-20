package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/drawer"
	"github.com/autonoco/buttons/internal/history"
	"github.com/autonoco/buttons/internal/webhook"
)

// summaryFlag is the universal --summary flag. When set on a mutating
// command, that command returns its dry-run plan instead of executing.
// When set on `buttons` itself (or via the summary subcommand), it
// dumps the workspace snapshot an agent uses to orient itself.
var summaryFlag bool

// summaryDeep expands inline schemas + all recent runs instead of the
// default compact shape. Off by default to keep context budget small.
var summaryDeep bool

// summaryCmd is the explicit `buttons summary` subcommand. Bare
// `buttons` is also wired to this (via rootCmd.RunE overriding
// when no positional args are passed) so the UX is:
//
//	buttons           → pretty summary
//	buttons summary   → same
//	buttons --json    → JSON summary
//	buttons <name>    → per-button detail (preserved existing behavior)
var summaryCmd = &cobra.Command{
	Use:   "summary",
	Short: "Print a workspace snapshot (buttons, drawers, recent runs)",
	Long: `Print a workspace snapshot. Default output is a compact
pretty-printed table; --json returns a structured response suitable
for agents. --deep inlines full schemas and all recent runs.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSummary()
	},
}

func init() {
	rootCmd.AddCommand(summaryCmd)
	rootCmd.PersistentFlags().BoolVar(&summaryFlag, "summary", false, "show a read-only plan/snapshot instead of mutating")
	summaryCmd.Flags().BoolVar(&summaryDeep, "deep", false, "inline full schemas + all recent runs")
}

// SummaryRequested returns true when --summary was passed. Mutating
// commands (press, drawer press, drawer add, drawer connect, etc.)
// check this and return a dry-run response instead of executing.
func SummaryRequested() bool { return summaryFlag }

func runSummary() error {
	btnSvc := button.NewService()
	dSvc := drawer.NewService()

	buttons, err := btnSvc.List()
	if err != nil {
		return handleServiceError(err)
	}
	drawers, err := dSvc.List()
	if err != nil {
		return handleDrawerError(err)
	}

	dataDir, _ := config.DataDir()
	profile := os.Getenv("BUTTONS_PROFILE")

	// Collect recent runs across all buttons for the "recent_failures"
	// bucket. Uses ListAll so we don't iterate the filesystem twice.
	allButtonRuns, _ := history.ListAll(50)
	failures := []map[string]any{}
	for _, r := range allButtonRuns {
		if r.Status != "ok" {
			failures = append(failures, map[string]any{
				"run_id":       r.StartedAt.Format("20060102-150405"),
				"target":       "button/" + r.ButtonName,
				"failed_step":  "",
				"error": map[string]any{
					"code":    r.ErrorType,
					"message": truncateStr(r.Stderr, 200),
				},
				"started_at":   r.StartedAt,
				"started_ago":  prettyDuration(time.Since(r.StartedAt)),
			})
			if len(failures) >= 5 {
				break
			}
		}
	}

	// Build button entries.
	btnEntries := make([]map[string]any, 0, len(buttons))
	for _, b := range buttons {
		recent, _ := history.List(b.Name, 3)
		btnEntries = append(btnEntries, buttonSummary(b, recent))
	}

	// Build drawer entries.
	drawerEntries := make([]map[string]any, 0, len(drawers))
	for _, d := range drawers {
		recent, _ := drawer.ListRuns(d.Name, 3)
		drawerEntries = append(drawerEntries, drawerSummaryEntry(d, recent))
	}

	// Webhook block: mode, hostname, and count of drawers routed.
	// Keeps the agent aware of whether the listener needs to run and
	// what URL prefix to expect.
	webhookBlock := buildWebhookSummaryBlock(drawers)

	resp := map[string]any{
		"version":  version,
		"data_dir": dataDir,
		"profile":  profile,
		"counts": map[string]int{
			"buttons":        len(buttons),
			"drawers":        len(drawers),
			"failures_recent": len(failures),
		},
		"buttons":          btnEntries,
		"drawers":          drawerEntries,
		"active_runs":      []any{}, // v1 executor is sync; nothing is ever active between commands
		"recent_failures":  failures,
		"schedules":        []any{}, // stage 3
		"webhook":          webhookBlock,
	}

	if jsonOutput {
		return config.WriteJSON(resp)
	}
	return printSummaryPretty(resp)
}

func buttonSummary(b button.Button, recent []history.Run) map[string]any {
	entry := map[string]any{
		"name":            b.Name,
		"description":     b.Description,
		"runtime":         b.Runtime,
		"args":            b.Args,
		"timeout_seconds": b.TimeoutSeconds,
	}
	if summaryDeep && len(b.OutputSchema) > 0 {
		var parsed any
		if err := json.Unmarshal(b.OutputSchema, &parsed); err == nil {
			entry["output_schema"] = parsed
		} else {
			entry["output_schema"] = nil
		}
	} else if len(b.OutputSchema) > 0 {
		entry["output_schema_ref"] = b.Name + ".output"
	}
	entry["recent_runs"] = summarizeButtonRuns(recent)
	return entry
}

// buildWebhookSummaryBlock rolls up webhook-listener state into the
// workspace summary. Agents use this to answer "should I start the
// listener?" and "what's my base URL?" without learning a second
// command.
func buildWebhookSummaryBlock(drawers []drawer.Drawer) map[string]any {
	cfg, _ := webhook.LoadConfig()
	mode := string(webhook.ModeQuick)
	hostname := ""
	if cfg != nil && cfg.Mode == webhook.ModeNamed {
		mode = string(webhook.ModeNamed)
		hostname = cfg.Hostname
	}

	routes := make([]map[string]any, 0)
	for _, d := range drawers {
		for _, t := range d.Triggers {
			if t.Kind != "webhook" || t.Path == "" {
				continue
			}
			entry := map[string]any{
				"drawer":     d.Name,
				"path":       t.Path,
				"has_secret": t.Secret != "",
			}
			if hostname != "" {
				entry["url"] = "https://" + hostname + t.Path
			}
			routes = append(routes, entry)
		}
	}

	block := map[string]any{
		"mode":         mode,
		"routes":       routes,
		"route_count":  len(routes),
		"listen_hint":  "buttons webhook listen",
	}
	if hostname != "" {
		block["hostname"] = hostname
		block["base_url"] = "https://" + hostname
	}
	return block
}

func drawerSummaryEntry(d drawer.Drawer, recent []drawer.Run) map[string]any {
	topology := make([]string, 0, len(d.Steps))
	for _, s := range d.Steps {
		topology = append(topology, s.ID)
	}
	entry := map[string]any{
		"name":        d.Name,
		"description": d.Description,
		"inputs":      d.Inputs,
		"topology":    topology,
	}
	if summaryDeep {
		entry["steps"] = d.Steps
	}
	if triggers := summarizeDrawerTriggers(&d); len(triggers) > 0 {
		entry["triggers"] = triggers
	}
	entry["recent_runs"] = summarizeDrawerRuns(recent)
	return entry
}

func summarizeButtonRuns(runs []history.Run) []map[string]any {
	out := make([]map[string]any, 0, len(runs))
	for _, r := range runs {
		entry := map[string]any{
			"id":          r.StartedAt.Format("20060102-150405"),
			"status":      r.Status,
			"duration_ms": r.DurationMs,
			"started_ago": prettyDuration(time.Since(r.StartedAt)),
		}
		if r.Status != "ok" && r.ErrorType != "" {
			entry["error"] = map[string]any{"code": r.ErrorType}
		}
		out = append(out, entry)
	}
	return out
}

func printSummaryPretty(resp map[string]any) error {
	fmt.Printf("buttons %s  ·  %s\n", version, resp["data_dir"])
	counts := resp["counts"].(map[string]int)
	fmt.Printf("  %d button(s), %d drawer(s)", counts["buttons"], counts["drawers"])
	if counts["failures_recent"] > 0 {
		fmt.Printf(", %d recent failure(s)", counts["failures_recent"])
	}
	fmt.Println()

	buttons := resp["buttons"].([]map[string]any)
	if len(buttons) > 0 {
		fmt.Println("\nbuttons:")
		for _, b := range buttons {
			fmt.Printf("  %s", b["name"])
			if d, ok := b["description"].(string); ok && d != "" {
				fmt.Printf(" — %s", d)
			}
			fmt.Printf("  (%s)\n", b["runtime"])
		}
	}

	drawers := resp["drawers"].([]map[string]any)
	if len(drawers) > 0 {
		fmt.Println("\ndrawers:")
		for _, d := range drawers {
			topology := d["topology"].([]string)
			fmt.Printf("  %s", d["name"])
			if s, ok := d["description"].(string); ok && s != "" {
				fmt.Printf(" — %s", s)
			}
			if len(topology) > 0 {
				fmt.Printf("\n    %s\n", joinStrs(topology, " → "))
			} else {
				fmt.Println()
			}
		}
	}

	failures := resp["recent_failures"].([]map[string]any)
	if len(failures) > 0 {
		fmt.Println("\nrecent failures:")
		for _, f := range failures {
			fmt.Printf("  ✗ %s  %s\n", f["target"], f["started_ago"])
			if errMap, ok := f["error"].(map[string]any); ok {
				if msg, ok := errMap["message"].(string); ok && msg != "" {
					fmt.Printf("    %s\n", msg)
				}
			}
		}
	}
	return nil
}

// prettyDuration returns "5m", "2h", "3d" — agent-friendly and
// context-cheap vs full ISO strings.
func prettyDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func joinStrs(xs []string, sep string) string {
	out := ""
	for i, s := range xs {
		if i > 0 {
			out += sep
		}
		out += s
	}
	return out
}
