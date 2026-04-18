package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/history"
	"github.com/autonoco/buttons/internal/tui"
)

// showButtonDetail displays the detail view for a single button.
//
// Three paths:
//
//   --json          structured detail dict (agents, pipes)
//   non-TTY         plain stderr printout (legacy / piped workflows)
//   TTY + human     full-screen Bubble Tea detail page (internal/tui)
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

	if config.IsNonTTY() {
		return renderDetailPlain(btn)
	}

	return renderDetailTUI(svc, btn)
}

// renderDetailTUI drops into the Bubble Tea detail page. On exit, if
// the user pressed `e`, shells out to $EDITOR on the resolved code
// path. Defers the exec until after Bubble Tea has restored the
// terminal so $EDITOR gets a clean TTY.
func renderDetailTUI(svc *button.Service, btn *button.Button) error {
	var lastRun *history.Run
	if runs, err := history.List(btn.Name, 1); err == nil && len(runs) > 0 {
		lastRun = &runs[0]
	}
	agentMD := readAgentMD(btn.Name)

	var codePath string
	if btn.URL == "" && btn.Runtime != "prompt" {
		if p, err := svc.CodePath(btn.Name); err == nil {
			codePath = p
		}
	} else if btn.Runtime == "prompt" {
		if dir, err := config.ButtonDir(btn.Name); err == nil {
			codePath = filepath.Join(dir, "AGENT.md")
		}
	}

	model := tui.NewDetail(btn, lastRun, agentMD, codePath)
	final, err := tui.RunDetail(model)
	if err != nil {
		return err
	}
	if final != nil && final.EditRequested() && codePath != "" {
		return openInEditor(codePath)
	}
	return nil
}

// openInEditor exec's $EDITOR (falls back to vi) against path. Runs
// inline — we inherit stdin/stdout/stderr so the editor gets a real
// TTY after Bubble Tea restored it.
func openInEditor(path string) error {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "vi"
	}
	// #nosec G204 -- editor binary is user-configured via $EDITOR /
	// $VISUAL (their own env); path is resolved via svc.CodePath or
	// config.ButtonDir which reject escapes. No shell interpolation.
	c := exec.Command(editor, path)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

// renderDetailPlain is the pre-TUI stderr printout. Kept for non-TTY
// (piped / CI) contexts where spinning up Bubble Tea would fail or
// look broken. Matches the exact shape of the output the CLI had
// before the TUI existed so scripts parsing it keep working.
func renderDetailPlain(btn *button.Button) error {
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
