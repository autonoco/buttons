package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/history"
)

// showButtonDetail displays the detail view for a single button.
func showButtonDetail(name string) error {
	svc := button.NewService()
	btn, err := svc.Get(name)
	if err != nil {
		return handleServiceError(err)
	}

	if jsonOutput {
		detail := map[string]any{
			"button": btn,
		}
		runs, err := history.List(btn.Name, 1)
		if err == nil && len(runs) > 0 {
			detail["last_run"] = runs[0]
		}
		if agentMD := readAgentMD(btn.Name); agentMD != "" {
			detail["agent_md"] = agentMD
		}
		return config.WriteJSON(detail)
	}

	// Human-readable detail view
	fmt.Fprintf(os.Stderr, "%s", btn.Name)
	if btn.Description != "" {
		fmt.Fprintf(os.Stderr, " -- %s", btn.Description)
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr)

	fmt.Fprintf(os.Stderr, "  Runtime:  %s\n", btn.Runtime)
	if btn.URL != "" {
		fmt.Fprintf(os.Stderr, "  Method:   %s\n", btn.Method)
		fmt.Fprintf(os.Stderr, "  URL:      %s\n", btn.URL)
		fmt.Fprintf(os.Stderr, "  Max resp: %s\n", button.FormatSize(button.ResolveMaxResponseBytes(btn.MaxResponseBytes)))
		netStatus := "blocked"
		if btn.AllowPrivateNetworks {
			netStatus = "allowed"
		}
		fmt.Fprintf(os.Stderr, "  Private:  %s\n", netStatus)
	}
	fmt.Fprintf(os.Stderr, "  Timeout:  %ds\n", btn.TimeoutSeconds)

	if len(btn.Args) > 0 {
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "  Args:")
		for _, arg := range btn.Args {
			req := "optional"
			if arg.Required {
				req = "required"
			}
			fmt.Fprintf(os.Stderr, "    %-16s %s  %s\n", arg.Name, arg.Type, req)
		}
	}

	// Usage examples (to stdout so agents can pipe)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "  Usage:")
	pressLine := fmt.Sprintf("buttons press %s", btn.Name)
	if len(btn.Args) > 0 {
		for _, arg := range btn.Args {
			if arg.Required {
				pressLine += fmt.Sprintf(" --arg %s=<%s>", arg.Name, arg.Type)
			}
		}
	}
	fmt.Fprintln(os.Stdout, pressLine)
	fmt.Fprintln(os.Stdout, pressLine+" --json")

	// Last run
	runs, err := history.List(btn.Name, 1)
	if err == nil && len(runs) > 0 {
		r := runs[0]
		fmt.Fprintln(os.Stderr)
		fmt.Fprintf(os.Stderr, "  Last run: %s -- %s (%dms)\n",
			r.StartedAt.Format("2006-01-02 15:04"),
			r.Status,
			r.DurationMs,
		)
	}

	return nil
}

func readAgentMD(buttonName string) string {
	btnDir, err := config.ButtonDir(buttonName)
	if err != nil {
		return ""
	}
	// #nosec G304 -- btnDir comes from config.ButtonDir() which rejects any
	// path escaping ButtonsDir; caller passes an already-slugified btn.Name.
	data, err := os.ReadFile(filepath.Join(btnDir, "AGENT.md"))
	if err != nil {
		return ""
	}
	return string(data)
}
