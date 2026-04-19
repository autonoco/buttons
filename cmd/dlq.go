package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/deadletter"
)

// dlqCmd is the parent for `buttons dlq` (dead letter queue).
// Final-failed press/drawer runs land here; an agent uses list +
// replay to triage and recover without scanning shell logs.
var dlqCmd = &cobra.Command{
	Use:   "dlq",
	Short: "Inspect and replay final-failed runs (dead letter queue)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var dlqListCmd = &cobra.Command{
	Use:   "list",
	Short: "List final-failed runs",
	RunE: func(cmd *cobra.Command, args []string) error {
		entries, err := deadletter.List(0)
		if err != nil {
			return handleServiceError(err)
		}
		if jsonOutput {
			return config.WriteJSON(entries)
		}
		if len(entries) == 0 {
			fmt.Fprintln(os.Stderr, "No failed runs in the DLQ.")
			return nil
		}
		for _, e := range entries {
			msg := e.Message
			if len(msg) > 80 {
				msg = msg[:80] + "…"
			}
			msg = strings.ReplaceAll(msg, "\n", " ")
			fmt.Printf("%s  %s  %s  %s\n", e.ID, e.Target, e.Code, msg)
		}
		return nil
	},
}

var dlqRemoveCmd = &cobra.Command{
	Use:   "remove <id>",
	Short: "Delete a DLQ entry (after out-of-band resolution)",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := deadletter.Remove(args[0]); err != nil {
			return handleServiceError(err)
		}
		if jsonOutput {
			return config.WriteJSON(map[string]any{"ok": true, "removed": args[0]})
		}
		fmt.Fprintf(os.Stderr, "Removed DLQ entry %s\n", args[0])
		return nil
	},
}

var dlqReplayCmd = &cobra.Command{
	Use:   "replay <id>",
	Short: "Replay a DLQ entry (prints the original command to run)",
	Long: `Print the command that would replay a DLQ entry. The actual
replay is intentionally left to the caller so agents can review the
inputs before re-running — the DLQ is a triage surface, not an
auto-retry daemon.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		e, err := deadletter.Get(args[0])
		if err != nil {
			return handleServiceError(err)
		}
		// Build the most likely replay command. For button targets we
		// reconstruct `buttons press <name> --arg k=v`; for drawers
		// it's `buttons drawer <name> press k=v`. Agents can accept
		// or modify before executing.
		parts := strings.SplitN(e.Target, "/", 2)
		if len(parts) != 2 {
			return fmt.Errorf("unrecognized DLQ target: %q", e.Target)
		}
		kind, name := parts[0], parts[1]
		var replay string
		switch kind {
		case "button":
			replay = fmt.Sprintf("buttons press %s", name)
			for k, v := range e.Inputs {
				replay += fmt.Sprintf(" --arg %s=%v", k, v)
			}
		case "drawer":
			replay = fmt.Sprintf("buttons drawer %s press", name)
			for k, v := range e.Inputs {
				replay += fmt.Sprintf(" %s=%v", k, v)
			}
		default:
			return fmt.Errorf("unknown DLQ kind: %q", kind)
		}
		if jsonOutput {
			return config.WriteJSON(map[string]any{
				"id":     e.ID,
				"target": e.Target,
				"replay": replay,
				"inputs": e.Inputs,
			})
		}
		fmt.Println(replay)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(dlqCmd)
	dlqCmd.AddCommand(dlqListCmd)
	dlqCmd.AddCommand(dlqRemoveCmd)
	dlqCmd.AddCommand(dlqReplayCmd)
}
