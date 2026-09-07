package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/drawer"
	"github.com/autonoco/buttons/internal/flowkit"
	"github.com/spf13/cobra"
)

var (
	flowOn       string
	flowManager  string
	flowWorker   string
	flowNoVerify bool
	flowPurge    bool
	flowFilter   string
	flowReason   string
	flowFailed   bool
	flowTaskID   string
	flowLimit    int
)

var flowCmd = &cobra.Command{
	Use:   "flow",
	Short: "Buttons Flow — pressable boards for drawer_kind:flow",
	Long: `Flow boards are drawer_kind:flow definitions that compile to an ordinary
drawer pipeline on every press. Verbs here are sugar over press/history/summary.`,
}

var flowEnsureButtonsCmd = &cobra.Command{
	Use:   "ensure-buttons",
	Short: "Install shared flow pipeline buttons for a provider",
	RunE: func(cmd *cobra.Command, args []string) error {
		provider := flowOn
		if provider == "" {
			provider = "local"
		}
		if err := flowkit.EnsureButtons(provider); err != nil {
			return err
		}
		if jsonOutput {
			return config.WriteJSON(map[string]any{"ok": true, "provider": provider})
		}
		fmt.Fprintf(os.Stderr, "flow buttons ready for provider %s\n", provider)
		return nil
	},
}

var flowInitCmd = &cobra.Command{
	Use:   "init NAME",
	Short: "Register triggers and prove a flow board presses",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return flowInit(args[0])
	},
}

var flowTaskCmd = &cobra.Command{
	Use:   "task",
	Short: "Task CRUD against a board's provider",
}

var flowStatusCmd = &cobra.Command{
	Use:   "status [NAME]",
	Short: "Board status summary",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := ""
		if len(args) == 1 {
			name = args[0]
		}
		return flowStatus(name)
	},
}

var flowLogsCmd = &cobra.Command{
	Use:   "logs [NAME]",
	Short: "Recent press history for a board",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := ""
		if len(args) == 1 {
			name = args[0]
		}
		return flowLogs(name)
	},
}

var flowApproveCmd = &cobra.Command{
	Use:   "approve BOARD TASK",
	Short: "Approve a gated transition",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return pressFlowHelper(args[0], "approve", map[string]string{"id": args[1]})
	},
}

var flowRejectCmd = &cobra.Command{
	Use:   "reject BOARD TASK",
	Short: "Reject a gated transition",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		argsMap := map[string]string{"id": args[1]}
		if flowReason != "" {
			argsMap["reason"] = flowReason
		}
		return pressFlowHelper(args[0], "reject", argsMap)
	},
}

var flowRmCmd = &cobra.Command{
	Use:   "rm NAME",
	Short: "Remove webhook trigger and schedule for a board",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return flowRm(args[0])
	},
}

func init() {
	flowInitCmd.Flags().StringVar(&flowOn, "on", "local", "provider: local|github")
	flowInitCmd.Flags().StringVar(&flowManager, "manager", "", "manager agent ref")
	flowInitCmd.Flags().StringVar(&flowWorker, "worker", "", "worker agent ref")
	flowInitCmd.Flags().BoolVar(&flowNoVerify, "no-verify", false, "skip proof press")
	flowEnsureButtonsCmd.Flags().StringVar(&flowOn, "on", "local", "provider: local|github")
	flowRmCmd.Flags().BoolVar(&flowPurge, "purge", false, "also delete local tasks")
	flowLogsCmd.Flags().BoolVar(&flowFailed, "failed", false, "only failed presses")
	flowLogsCmd.Flags().StringVar(&flowTaskID, "task", "", "filter by task id (best-effort)")
	flowLogsCmd.Flags().IntVar(&flowLimit, "limit", 20, "max history entries")
	flowRejectCmd.Flags().StringVar(&flowReason, "reason", "", "rejection reason")

	flowTaskCmd.AddCommand(
		&cobra.Command{Use: "add BOARD TITLE", Args: cobra.MinimumNArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
			return pressFlowHelper(args[0], "task-add", map[string]string{"title": strings.Join(args[1:], " ")})
		}},
		&cobra.Command{Use: "list BOARD", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			m := map[string]string{}
			if flowFilter != "" {
				m["filter"] = flowFilter
			}
			return pressFlowHelper(args[0], "task-list", m)
		}},
		&cobra.Command{Use: "read BOARD ID", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
			return pressFlowHelper(args[0], "task-read", map[string]string{"id": args[1]})
		}},
		&cobra.Command{Use: "update BOARD ID PATCH_JSON", Args: cobra.ExactArgs(3), RunE: func(cmd *cobra.Command, args []string) error {
			return pressFlowHelper(args[0], "task-update", map[string]string{"id": args[1], "patch": args[2]})
		}},
		&cobra.Command{Use: "rm BOARD ID", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
			return pressFlowHelper(args[0], "task-rm", map[string]string{"id": args[1]})
		}},
		&cobra.Command{Use: "claim BOARD ID", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
			return pressFlowHelper(args[0], "task-claim", map[string]string{
				"task":    fmt.Sprintf(`{"id":%q}`, args[1]),
				"task_id": args[1],
			})
		}},
		&cobra.Command{Use: "comment BOARD ID BODY", Args: cobra.MinimumNArgs(3), RunE: func(cmd *cobra.Command, args []string) error {
			return pressFlowHelper(args[0], "task-comment", map[string]string{"id": args[1], "body": strings.Join(args[2:], " ")})
		}},
	)
	flowTaskCmd.PersistentFlags().StringVar(&flowFilter, "filter", "", "key=value filter (list)")

	flowCmd.AddCommand(flowEnsureButtonsCmd, flowInitCmd, flowTaskCmd, flowStatusCmd, flowLogsCmd, flowApproveCmd, flowRejectCmd, flowRmCmd)
	rootCmd.AddCommand(flowCmd)
}

func flowProviderFor(d *drawer.Drawer) string {
	if d != nil && d.Flow != nil && d.Flow.Provider != "" {
		return d.Flow.Provider
	}
	if flowOn != "" {
		return flowOn
	}
	return "local"
}

func pressFlowHelper(board, verb string, args map[string]string) error {
	dsvc := drawer.NewService()
	d, err := dsvc.Get(board)
	if err != nil {
		// allow task ops before drawer exists only for ensure; otherwise require board
		return handleDrawerError(err)
	}
	provider := flowProviderFor(d)
	if err := flowkit.EnsureButtons(provider); err != nil {
		return err
	}
	btnName := "flow-" + provider + "-" + verb
	argList := []string{"board=" + board}
	for k, v := range args {
		argList = append(argList, k+"="+v)
	}
	// Reuse buttons press machinery by invoking the button service path.
	return runPressButton(btnName, argList)
}

func runPressButton(name string, argList []string) error {
	bin, err := os.Executable()
	if err != nil {
		bin = "buttons"
	}
	cmdArgs := []string{"press", name}
	if jsonOutput {
		cmdArgs = append(cmdArgs, "--json")
	}
	for _, kv := range argList {
		cmdArgs = append(cmdArgs, "--arg", kv)
	}
	c := exec.Command(bin, cmdArgs...) // #nosec G204
	c.Env = os.Environ()
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return errSilent
	}
	return nil
}

func flowInit(name string) error {
	provider := flowOn
	if provider == "" {
		provider = "local"
	}
	if err := flowkit.EnsureButtons(provider); err != nil {
		return err
	}

	dsvc := drawer.NewService()
	d, err := dsvc.Get(name)
	if err != nil {
		// Install research-deck template when asking for it and missing.
		if name == "research-deck" {
			if err := installResearchDeckBoard(provider); err != nil {
				return handleDrawerError(&drawer.ServiceError{Code: "VALIDATION_ERROR", Message: err.Error()})
			}
			d, err = dsvc.Get(name)
		}
		if err != nil {
			return handleDrawerError(err)
		}
	}
	if d.DrawerKind != drawer.DrawerKindFlow {
		return handleDrawerError(&drawer.ServiceError{Code: "DRAWER_KIND_MISMATCH", Message: fmt.Sprintf("%q is not a flow drawer", name)})
	}

	if _, err := dsvc.SetFlowField(name, "provider", provider); err != nil {
		return handleDrawerError(err)
	}
	if flowManager != "" {
		if _, err := dsvc.SetFlowField(name, "manager.agent", flowManager); err != nil {
			return handleDrawerError(err)
		}
	}
	if flowWorker != "" {
		// apply to stages that have workers unset — set first stage worker
		if len(d.Flow.Stages) > 0 {
			path := "stages." + d.Flow.Stages[0].ID + ".worker.agent"
			if _, err := dsvc.SetFlowField(name, path, flowWorker); err != nil {
				return handleDrawerError(err)
			}
		}
	}

	// Webhook trigger on the flow drawer itself.
	path := "/" + name
	if _, err := dsvc.SetWebhookTrigger(name, path, nil); err != nil {
		return handleDrawerError(err)
	}
	if err := writeFlowSchedule(name, provider); err != nil {
		return err
	}
	if err := flowkit.EnsureTasksDir(name); err != nil {
		return err
	}

	if !flowNoVerify {
		// Throwaway proof task + press. Suppress nested JSON so --json
		// on init returns a single envelope.
		prevJSON := jsonOutput
		jsonOutput = false
		errProof := pressFlowHelper(name, "task-add", map[string]string{"title": "flow-init-proof"})
		if errProof == nil {
			errProof = drawerPress(name, nil)
		}
		jsonOutput = prevJSON
		if errProof != nil {
			return errProof
		}
	}

	if jsonOutput {
		return config.WriteJSON(map[string]any{
			"ok":       true,
			"board":    name,
			"provider": provider,
			"webhook":  path,
		})
	}
	fmt.Fprintf(os.Stderr, "flow %s initialized on %s (webhook %s)\n", name, provider, path)
	printNextHint("buttons flow task add %s \"...\"", name)
	return nil
}

func writeFlowSchedule(board, provider string) error {
	boardDir, err := config.FlowBoardDir(board)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(boardDir, 0o700); err != nil {
		return err
	}
	everySeconds := 60
	if provider == "github" {
		everySeconds = 300
	}
	meta := map[string]any{
		"board":         board,
		"provider":      provider,
		"kind":          "poll",
		"every_seconds": everySeconds,
		"command":       fmt.Sprintf("buttons drawer %s press", board),
	}
	raw, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(filepath.Join(boardDir, "schedule.json"), append(raw, '\n'), 0o600); err != nil {
		return err
	}

	switch provider {
	case "github":
		cronMinutes := everySeconds / 60
		if cronMinutes < 1 {
			cronMinutes = 1
		}
		wf := fmt.Sprintf(`name: buttons-flow-%s
on:
  schedule:
    - cron: "*/%d * * * *"
  workflow_dispatch:
jobs:
  press:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Press flow board
        run: buttons drawer %s press
`, board, cronMinutes, board)
		wfDir := filepath.Join(boardDir, "github-actions")
		_ = os.MkdirAll(wfDir, 0o700)
		return os.WriteFile(filepath.Join(wfDir, "press.yml"), []byte(wf), 0o600)
	default:
		// Platform-native local schedule hint file (launchd/crontab/systemd).
		bin, _ := os.Executable()
		if bin == "" {
			bin = "buttons"
		}
		switch runtime.GOOS {
		case "darwin":
			plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>sh.buttons.flow.%s</string>
  <key>ProgramArguments</key><array>
    <string>%s</string><string>drawer</string><string>%s</string><string>press</string>
  </array>
  <key>StartInterval</key><integer>60</integer>
</dict></plist>
`, board, bin, board)
			return os.WriteFile(filepath.Join(boardDir, "launchd.plist"), []byte(plist), 0o600)
		default:
			line := fmt.Sprintf("* * * * * %s drawer %s press\n", bin, board)
			return os.WriteFile(filepath.Join(boardDir, "crontab.line"), []byte(line), 0o600)
		}
	}
}

func flowStatus(name string) error {
	dsvc := drawer.NewService()
	boards := []string{}
	if name != "" {
		boards = []string{name}
	} else {
		list, err := dsvc.List()
		if err != nil {
			return err
		}
		for _, d := range list {
			if d.DrawerKind == drawer.DrawerKindFlow {
				boards = append(boards, d.Name)
			}
		}
	}
	out := make([]map[string]any, 0, len(boards))
	for _, b := range boards {
		d, err := dsvc.Get(b)
		if err != nil {
			continue
		}
		provider := flowProviderFor(d)
		_ = flowkit.EnsureButtons(provider)
		// counts via task-list
		entry := map[string]any{"name": b, "provider": provider}
		if d.Flow != nil {
			entry["initial_stage"] = d.Flow.InitialStage
			entry["stages"] = len(d.Flow.Stages)
		}
		boardDir, _ := config.FlowBoardDir(b)
		sched := filepath.Join(boardDir, "schedule.json")
		if _, err := os.Stat(sched); err != nil {
			entry["trigger_drift"] = "schedule missing"
		} else {
			entry["schedule"] = sched
		}
		hasWebhook := false
		for _, t := range d.Triggers {
			if t.Kind == "webhook" {
				hasWebhook = true
				entry["webhook"] = t.Path
			}
		}
		if !hasWebhook {
			if drift, ok := entry["trigger_drift"].(string); ok {
				entry["trigger_drift"] = drift + "; webhook missing"
			} else {
				entry["trigger_drift"] = "webhook missing"
			}
		}
		out = append(out, entry)
	}
	if jsonOutput {
		return config.WriteJSON(map[string]any{"ok": true, "boards": out})
	}
	for _, e := range out {
		fmt.Fprintf(os.Stderr, "%s provider=%v stages=%v\n", e["name"], e["provider"], e["stages"])
		if drift, ok := e["trigger_drift"]; ok {
			fmt.Fprintf(os.Stderr, "  trigger drift: %v\n", drift)
		}
	}
	return nil
}

func flowLogs(name string) error {
	if name == "" {
		if jsonOutput {
			_ = config.WriteJSONError("MISSING_ARG", "board name required")
			return errSilent
		}
		return fmt.Errorf("MISSING_ARG: board name required")
	}
	entries, err := drawer.ListRuns(name, flowLimit)
	if err != nil {
		return err
	}
	filtered := make([]drawer.Run, 0, len(entries))
	for _, e := range entries {
		if flowFailed && e.Status == "ok" {
			continue
		}
		if flowTaskID != "" {
			raw, _ := json.Marshal(e)
			if !strings.Contains(string(raw), flowTaskID) {
				continue
			}
		}
		filtered = append(filtered, e)
	}
	if jsonOutput {
		return config.WriteJSON(map[string]any{"ok": true, "runs": filtered})
	}
	for _, e := range filtered {
		fmt.Fprintf(os.Stderr, "%s  %s  %dms  started=%s\n",
			e.RunID, e.Status, e.DurationMs, e.StartedAt.Format("2006-01-02T15:04:05Z"))
	}
	return nil
}

func flowRm(name string) error {
	dsvc := drawer.NewService()
	if _, err := dsvc.ClearTriggers(name); err != nil {
		return handleDrawerError(err)
	}
	boardDir, err := config.FlowBoardDir(name)
	if err == nil {
		_ = os.Remove(filepath.Join(boardDir, "schedule.json"))
		_ = os.Remove(filepath.Join(boardDir, "launchd.plist"))
		_ = os.Remove(filepath.Join(boardDir, "crontab.line"))
		_ = os.RemoveAll(filepath.Join(boardDir, "github-actions"))
		if flowPurge {
			_ = os.RemoveAll(filepath.Join(boardDir, "tasks"))
		}
	}
	if jsonOutput {
		return config.WriteJSON(map[string]any{"ok": true, "removed": name, "purged": flowPurge})
	}
	fmt.Fprintf(os.Stderr, "removed triggers/schedule for %s\n", name)
	return nil
}
