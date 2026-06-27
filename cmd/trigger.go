package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/trigger"
	"github.com/spf13/cobra"
)

var triggerCmd = &cobra.Command{
	Use:   "trigger",
	Short: "Manage button triggers (cron, watch, webhook)",
	Long: `Attach event triggers to a button so it presses automatically.

Triggers run under 'buttons serve':
  - cron    fires on a schedule (5-field cron, in-process scheduler)
  - watch   fires when a file changes (polled, 500ms debounce)
  - webhook fires on an HTTP POST to a path on the serve listener

Examples:
  buttons trigger add health --cron --schedule "0 */6 * * *"
  buttons trigger add reindex --watch --path ./data.json
  buttons trigger add deploy --webhook --webhook-path /hooks/deploy --token s3cr3t
  buttons trigger list
  buttons trigger rm health <trigger-id>`,
}

var triggerAddFlags struct {
	webhook     bool
	cron        bool
	watch       bool
	schedule    string
	webhookPath string
	watchPath   string
	token       string
	args        []string
}

var triggerAddCmd = &cobra.Command{
	Use:   "add <button>",
	Short: "Add a trigger to a button",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		if boolCount(triggerAddFlags.cron, triggerAddFlags.watch, triggerAddFlags.webhook) != 1 {
			return cliErr("pass exactly one of --cron, --watch, or --webhook")
		}
		var tr button.Trigger
		switch {
		case triggerAddFlags.cron:
			tr = button.Trigger{Kind: trigger.KindCron, Schedule: triggerAddFlags.schedule}
		case triggerAddFlags.watch:
			tr = button.Trigger{Kind: trigger.KindWatch, Path: triggerAddFlags.watchPath}
		case triggerAddFlags.webhook:
			tr = button.Trigger{Kind: trigger.KindWebhook, Path: triggerAddFlags.webhookPath, Token: triggerAddFlags.token}
		}

		parsedArgs, err := parseTriggerArgs(triggerAddFlags.args)
		if err != nil {
			return cliErr(err.Error())
		}
		tr.Args = parsedArgs

		svc := button.NewService()
		created, err := trigger.Add(svc, name, tr)
		if err != nil {
			return handleServiceError(err)
		}

		if jsonOutput {
			return config.WriteJSON(map[string]any{"button": name, "trigger": created})
		}
		fmt.Fprintf(os.Stderr, "Added %s trigger %s to %q\n", created.Kind, created.ID, name)
		printNextHint("buttons trigger list")
		return nil
	},
}

var triggerListCmd = &cobra.Command{
	Use:   "list [button]",
	Short: "List triggers (all buttons, or one)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc := button.NewService()
		if len(args) == 1 {
			trs, err := trigger.List(svc, args[0])
			if err != nil {
				return handleServiceError(err)
			}
			if jsonOutput {
				return config.WriteJSON(map[string]any{"button": args[0], "triggers": trs})
			}
			if len(trs) == 0 {
				fmt.Fprintf(os.Stderr, "no triggers on %q\n", args[0])
				return nil
			}
			for _, t := range trs {
				fmt.Fprintf(os.Stderr, "  %s  %-7s  %s\n", t.ID, t.Kind, triggerDetail(t))
			}
			return nil
		}

		all, err := trigger.ListAll(svc)
		if err != nil {
			return handleServiceError(err)
		}
		if jsonOutput {
			return config.WriteJSON(map[string]any{"triggers": all})
		}
		if len(all) == 0 {
			fmt.Fprintln(os.Stderr, "no triggers configured")
			return nil
		}
		for _, b := range all {
			fmt.Fprintf(os.Stderr, "  %s  %-7s  %-20s  %s\n", b.Trigger.ID, b.Trigger.Kind, b.Button, triggerDetail(b.Trigger))
		}
		return nil
	},
}

var triggerRmCmd = &cobra.Command{
	Use:   "rm <button> <trigger-id>",
	Short: "Remove a trigger from a button",
	Args:  exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc := button.NewService()
		if err := trigger.Remove(svc, args[0], args[1]); err != nil {
			return handleServiceError(err)
		}
		if jsonOutput {
			return config.WriteJSON(map[string]any{"removed": args[1], "button": args[0]})
		}
		fmt.Fprintf(os.Stderr, "Removed trigger %s from %q\n", args[1], args[0])
		return nil
	},
}

func triggerDetail(t button.Trigger) string {
	switch t.Kind {
	case trigger.KindCron:
		return t.Schedule
	case trigger.KindWatch:
		return "watch " + t.Path
	case trigger.KindWebhook:
		if t.Token != "" {
			return "POST " + t.Path + " (token)"
		}
		return "POST " + t.Path
	}
	return ""
}

func boolCount(bs ...bool) int {
	n := 0
	for _, b := range bs {
		if b {
			n++
		}
	}
	return n
}

func parseTriggerArgs(pairs []string) (map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(pairs))
	for _, p := range pairs {
		i := strings.Index(p, "=")
		if i <= 0 {
			return nil, fmt.Errorf("arg %q must be key=value", p)
		}
		out[p[:i]] = p[i+1:]
	}
	return out, nil
}

func cliErr(msg string) error {
	if jsonOutput {
		_ = config.WriteJSONError("VALIDATION_ERROR", msg)
		return errSilent
	}
	return fmt.Errorf("%s", msg)
}

func init() {
	triggerAddCmd.Flags().BoolVar(&triggerAddFlags.cron, "cron", false, "cron-scheduled trigger")
	triggerAddCmd.Flags().BoolVar(&triggerAddFlags.watch, "watch", false, "file-watch trigger")
	triggerAddCmd.Flags().BoolVar(&triggerAddFlags.webhook, "webhook", false, "webhook (HTTP POST) trigger")
	triggerAddCmd.Flags().StringVar(&triggerAddFlags.schedule, "schedule", "", "cron schedule, 5-field (e.g. \"0 */6 * * *\")")
	triggerAddCmd.Flags().StringVar(&triggerAddFlags.webhookPath, "webhook-path", "", "URL path for the webhook (e.g. /hooks/deploy)")
	triggerAddCmd.Flags().StringVar(&triggerAddFlags.watchPath, "path", "", "file path to watch")
	triggerAddCmd.Flags().StringVar(&triggerAddFlags.token, "token", "", "shared secret required on webhook POSTs (X-Buttons-Token or ?token=)")
	triggerAddCmd.Flags().StringArrayVar(&triggerAddFlags.args, "arg", nil, "argument key=value passed when the trigger fires (repeatable)")

	triggerCmd.AddCommand(triggerAddCmd)
	triggerCmd.AddCommand(triggerListCmd)
	triggerCmd.AddCommand(triggerRmCmd)
	rootCmd.AddCommand(triggerCmd)
}
