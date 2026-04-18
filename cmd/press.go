package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/engine"
	"github.com/autonoco/buttons/internal/history"
	"github.com/spf13/cobra"
)

var pressArgs []string
var pressTimeout int
var pressDryRun bool

var pressCmd = &cobra.Command{
	Use:   "press [name]",
	Short: "Run a button",
	Long: `Run a button by name.

Executes the action defined by the button, passing arguments as environment
variables (BUTTONS_ARG_<NAME>) for code buttons, or as template substitutions
for API buttons. Returns structured output in --json mode.

Common flags:
      --arg KEY=VALUE   pass an argument (repeatable; validated against the spec)
      --timeout SECS    override the button's configured timeout for this press
      --dry-run         print what would run without executing
      --json            emit machine-readable output (default when stdout is piped)

Examples:
  buttons press deploy --arg env=production
  buttons press weather --arg city=Miami --json
  buttons press deploy --dry-run
  buttons press slow-task --timeout 120`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		svc := button.NewService()

		btn, err := svc.Get(name)
		if err != nil {
			se, ok := err.(*button.ServiceError)
			if ok && se.Code == "NOT_FOUND" && jsonOutput {
				_ = config.WriteJSONErrorWithHint(se.Code, se.Message,
					"run 'buttons list --json' to see available buttons", nil)
				return errSilent
			}
			return handleServiceError(err)
		}

		// Parse and validate args
		parsedArgs, err := button.ParsePressArgs(pressArgs, btn.Args)
		if err != nil {
			se, ok := err.(*button.ServiceError)
			if ok && jsonOutput {
				_ = config.WriteJSONErrorWithHint(se.Code, se.Message,
					fmt.Sprintf("run 'buttons %s --json' to see the full button spec", btn.Name),
					btn.Args,
				)
				return errSilent
			}
			return handleServiceError(err)
		}

		// Resolve timeout
		timeout := btn.TimeoutSeconds
		if pressTimeout > 0 {
			timeout = pressTimeout
		}

		// Dry run
		if pressDryRun {
			return dryRun(btn, parsedArgs, timeout)
		}

		// Resolve code/prompt path for non-HTTP buttons
		var codePath string
		if btn.URL == "" {
			if btn.Runtime == "prompt" {
				btnDir, err := config.ButtonDir(btn.Name)
				if err != nil {
					return handleServiceError(err)
				}
				codePath = filepath.Join(btnDir, "AGENT.md")
			} else {
				codePath, err = svc.CodePath(btn.Name)
				if err != nil {
					return handleServiceError(err)
				}
			}
		}

		// Execute
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
		defer cancel()

		// Load batteries so secrets can reach the press as BUTTONS_BAT_<KEY>
		// without the user hardcoding them in the script. A resolution error
		// (e.g. unreadable batteries.json) should not silently skip; surface
		// it with the rest of the press's structured-error handling.
		batSvc, err := newBatteryService()
		if err != nil {
			return handleServiceError(err)
		}
		batteries, err := batSvc.Env()
		if err != nil {
			return handleServiceError(err)
		}

		result := engine.Execute(ctx, btn, parsedArgs, batteries, codePath)

		// Attach prompt if AGENT.md has custom content (not the default template)
		if promptMD := readPrompt(btn.Name); promptMD != "" {
			result.Prompt = promptMD
		}

		// Persist run history (non-fatal)
		if err := history.Record(result); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to write history: %v\n", err)
		}

		if jsonOutput {
			if result.Status == "ok" {
				return config.WriteJSON(result)
			}
			_ = config.WriteJSONError(result.ErrorType, result.Stderr)
			return errSilent
		}

		// Human-readable output
		switch result.Status {
		case "ok":
			fmt.Fprintf(os.Stderr, "✓ %s completed in %dms (exit %d)\n", btn.Name, result.DurationMs, result.ExitCode)
			if result.Stdout != "" {
				fmt.Fprint(os.Stdout, result.Stdout)
			}
			printNextHint("buttons history %s", btn.Name)
			return nil
		case "error":
			fmt.Fprintf(os.Stderr, "✗ %s failed (exit %d) in %dms\n", btn.Name, result.ExitCode, result.DurationMs)
			if result.Stderr != "" {
				fmt.Fprintf(os.Stderr, "stderr: %s", result.Stderr)
				if !strings.HasSuffix(result.Stderr, "\n") {
					fmt.Fprintln(os.Stderr)
				}
			}
			return errSilent
		case "timeout":
			fmt.Fprintf(os.Stderr, "✗ %s timed out after %ds\n", btn.Name, timeout)
			return errSilent
		}

		return nil
	},
}

func dryRun(btn *button.Button, args map[string]string, timeout int) error {
	info := map[string]any{
		"button":  btn.Name,
		"runtime": btn.Runtime,
		"timeout": timeout,
		"args":    args,
	}
	if btn.URL != "" {
		// Same encoding path as the real executor, so dry-run output
		// reflects exactly what will be sent.
		resolvedURL := engine.SubstituteURL(btn.URL, args)
		info["url"] = resolvedURL
		info["method"] = btn.Method
	} else {
		info["env"] = buildEnvMap(btn, args)
	}

	if jsonOutput {
		return config.WriteJSON(info)
	}

	fmt.Fprintf(os.Stderr, "Dry run: %s\n", btn.Name)
	fmt.Fprintf(os.Stderr, "  runtime:  %s\n", btn.Runtime)
	if btn.URL != "" {
		resolvedURL := engine.SubstituteURL(btn.URL, args)
		fmt.Fprintf(os.Stderr, "  method:   %s\n", btn.Method)
		fmt.Fprintf(os.Stderr, "  url:      %s\n", resolvedURL)
	}
	fmt.Fprintf(os.Stderr, "  timeout:  %ds\n", timeout)
	if len(args) > 0 {
		fmt.Fprintf(os.Stderr, "  args:\n")
		for k, v := range args {
			envName := "BUTTONS_ARG_" + strings.ToUpper(strings.ReplaceAll(k, "-", "_"))
			fmt.Fprintf(os.Stderr, "    %s=%s → %s\n", k, v, envName)
		}
	}
	return nil
}

func readPrompt(buttonName string) string {
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
	content := strings.TrimSpace(string(data))
	// Skip default template content
	if strings.Contains(content, "_Add context about this button here") {
		return ""
	}
	return content
}

func buildEnvMap(btn *button.Button, args map[string]string) map[string]string {
	env := make(map[string]string, len(btn.Env)+len(args))
	for k, v := range btn.Env {
		env[k] = v
	}
	for k, v := range args {
		envName := "BUTTONS_ARG_" + strings.ToUpper(strings.ReplaceAll(k, "-", "_"))
		env[envName] = v
	}
	return env
}

func init() {
	pressCmd.Flags().StringArrayVar(&pressArgs, "arg", nil, "argument as key=value")
	pressCmd.Flags().IntVar(&pressTimeout, "timeout", 0, "override timeout in seconds")
	pressCmd.Flags().BoolVar(&pressDryRun, "dry-run", false, "show what would execute without running")
	rootCmd.AddCommand(pressCmd)
}
