package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/settings"
	"github.com/spf13/cobra"
)

// builtinCreateTimeoutDefault is the fallback timeout when the user
// hasn't set one via 'buttons config set default-timeout' and didn't
// pass --timeout explicitly. Kept close to the flag so the resolution
// chain is readable in one place.
const builtinCreateTimeoutDefault = 300

var createFile string
var createCode string
var createRuntime string
var createURL string
var createMethod string
var createHeaders []string
var createBody string
var createPrompt string
var createDescription string
var createTimeout int
var createMaxResponseSize string
var createAllowPrivateNetworks bool
var createArgs []string
var createIgnore bool

var createCmd = &cobra.Command{
	Use:   "create [name]",
	Short: "Create a new button",
	Long: `Create a new button.

By default, 'buttons create <name>' scaffolds a shell button with a
placeholder main.sh the agent can edit, then press. Use --runtime to
scaffold a Python or Node button instead.

Provide a shortcut flag to skip the placeholder: --code for a one-line
inline script, --file to copy an existing script, --url for an HTTP
endpoint, or --prompt for a standalone instruction.

Arguments are defined with --arg in name:type:required|optional format.
Supported types: string, int, bool, enum.

Enum args accept a 4th pipe-separated segment listing the allowed
values — the TUI press form renders them as a horizontal choice row,
and the CLI validates the supplied value is in the set:

  --arg env:enum:required:staging|prod|canary

Args are injected as env vars for scripts or substituted into URL
templates for API buttons.

Common flags:
  -f, --file PATH       copy an existing script file as this button's code
      --code STRING     inline script body (shortcut for one-liners)
      --url URL         turn this into an HTTP button
      --arg SPEC        define an arg (name:type:required|optional,
                        or name:enum:required:a|b|c; repeatable)
      --timeout SECS    execution timeout (default: 300)
  -d, --description S   human-readable description for 'buttons list'
      --runtime NAME    shell | python | node  (default: shell)

Examples:
  buttons create deploy --arg env:enum:required:staging|prod|canary   # enum arg
  buttons create deploy                                  # scaffold, then edit main.sh
  buttons create etl --runtime python                    # scaffold, then edit main.py
  buttons create greet --code 'echo "Hello, $BUTTONS_ARG_NAME"' --arg name:string:required
  buttons create k8s-deploy -f ./scripts/deploy.sh --arg env:string:required
  buttons create weather --url 'https://wttr.in/{{city}}?format=j1' --arg city:string:required
  buttons create graphql --url https://api.example.com/graphql --method POST \
    --header "Content-Type: application/json" --body '{"query": "{ viewer { login } }"}'
  buttons create check-logs --prompt "Use the Northflank CLI to read production logs and summarize errors"`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		code := createCode

		argDefs, err := button.ParseArgDefs(createArgs)
		if err != nil {
			return handleServiceError(err)
		}

		// Resolve effective timeout: explicit --timeout flag > user
		// setting > built-in. cmd.Flags().Changed() is how we tell
		// "user passed --timeout" apart from "user accepted the
		// flag's default value" — matters because the flag's default
		// is itself a fallback we want settings to override.
		timeout := resolveCreateTimeout(cmd)

		var maxResponseBytes int64
		if createMaxResponseSize != "" {
			maxResponseBytes, err = button.ParseSize(createMaxResponseSize)
			if err != nil {
				return handleServiceError(&button.ServiceError{Code: "VALIDATION_ERROR", Message: fmt.Sprintf("--max-response-size: %v", err)})
			}
		}

		// Parse headers (Key: Value format)
		headers := make(map[string]string, len(createHeaders))
		for _, h := range createHeaders {
			idx := strings.Index(h, ":")
			if idx < 0 {
				return handleServiceError(&button.ServiceError{Code: "VALIDATION_ERROR", Message: fmt.Sprintf("header %q must have format 'Key: Value'", h)})
			}
			headers[strings.TrimSpace(h[:idx])] = strings.TrimSpace(h[idx+1:])
		}

		svc := button.NewService()
		btn, err := svc.Create(button.CreateOpts{
			Name:                 args[0],
			FilePath:             createFile,
			Code:                 code,
			Runtime:              createRuntime,
			URL:                  createURL,
			Method:               createMethod,
			Headers:              headers,
			Body:                 createBody,
			Prompt:               createPrompt,
			Description:          createDescription,
			TimeoutSeconds:       timeout,
			MaxResponseBytes:     maxResponseBytes,
			AllowPrivateNetworks: createAllowPrivateNetworks,
			Args:                 argDefs,
		})
		if err != nil {
			return handleServiceError(err)
		}

		// Resolve the on-disk paths for the created button so callers can
		// jump straight to the code file (scaffolded or otherwise).
		btnDir, _ := config.ButtonDir(btn.Name)
		var codePath string
		if btn.URL == "" && btn.Runtime != "prompt" {
			if p, err := svc.CodePath(btn.Name); err == nil {
				codePath = p
			}
		}

		// --ignore: add this button to .buttons/.gitignore so git
		// won't track it. Best-effort — failure here shouldn't fail
		// the overall create (the button is already on disk).
		ignored := false
		if createIgnore {
			entry, nerr := normalizeIgnoreTarget(btn.Name)
			if nerr == nil {
				if added, ierr := addIgnoreEntry(entry); ierr == nil {
					ignored = added
				} else {
					fmt.Fprintf(os.Stderr, "warning: --ignore requested but could not write .gitignore: %v\n", ierr)
				}
			}
		}

		if jsonOutput {
			return config.WriteJSON(struct {
				*button.Button
				CodePath  string `json:"code_path,omitempty"`
				ButtonDir string `json:"button_dir"`
				Ignored   bool   `json:"ignored,omitempty"`
			}{
				Button:    btn,
				CodePath:  codePath,
				ButtonDir: btnDir,
				Ignored:   ignored,
			})
		}

		fmt.Fprintf(os.Stderr, "Created button: %s\n", btn.Name)
		if ignored {
			fmt.Fprintf(os.Stderr, "  (added to .buttons/.gitignore — not tracked by git)\n")
		}
		if codePath != "" {
			fmt.Fprintf(os.Stderr, "  Edit:  %s\n", codePath)
		} else if btn.Runtime == "prompt" {
			fmt.Fprintf(os.Stderr, "  Edit:  %s\n", filepath.Join(btnDir, "AGENT.md"))
		}
		// Build a press hint that's actually runnable — if the button
		// has required args, stub them so the user can fill in values.
		pressExample := "buttons press " + btn.Name
		for _, a := range btn.Args {
			if a.Required {
				pressExample += fmt.Sprintf(" --arg %s=<%s>", a.Name, a.Type)
			}
		}
		printNextHint(pressExample)
		return nil
	},
}

// resolveCreateTimeout picks the effective --timeout for this press:
//
//  1. Explicit --timeout flag on the command line.
//  2. 'default-timeout' from ~/.buttons/settings.json.
//  3. builtinCreateTimeoutDefault (300).
//
// Settings-read errors fall through silently to the built-in default
// rather than failing the command — a bad settings file should never
// block `buttons create`.
func resolveCreateTimeout(cmd *cobra.Command) int {
	if cmd.Flags().Changed("timeout") {
		return createTimeout
	}
	if svc, err := settings.NewServiceFromEnv(); err == nil {
		if st, err := svc.Load(); err == nil {
			if v, ok := st.DefaultTimeout(); ok {
				return v
			}
		}
	}
	return builtinCreateTimeoutDefault
}

func init() {
	createCmd.Flags().StringVarP(&createFile, "file", "f", "", "copy an existing script file into the button folder")
	createCmd.Flags().StringVar(&createCode, "code", "", "inline script code (shortcut for one-liners)")
	createCmd.Flags().StringVar(&createRuntime, "runtime", "", "code runtime: shell, python, node (default: shell)")
	createCmd.Flags().StringVar(&createURL, "url", "", "HTTP API endpoint URL (supports {{arg}} templates)")
	createCmd.Flags().StringVar(&createMethod, "method", "", "HTTP method for --url (default: GET)")
	createCmd.Flags().StringArrayVar(&createHeaders, "header", nil, "HTTP header as 'Key: Value' (repeatable)")
	createCmd.Flags().StringVar(&createBody, "body", "", "HTTP request body (supports {{arg}} templates)")
	createCmd.Flags().StringVar(&createPrompt, "prompt", "", "prompt/instruction for the consuming agent (written to AGENT.md)")
	createCmd.Flags().StringVarP(&createDescription, "description", "d", "", "button description")
	// 300s default: the old 60s cap bit real agent workloads — ETL jobs,
	// long curls waiting on third-party APIs, DB migrations. Safety still
	// comes from *having* a timeout, not from it being tight; users can
	// shorten at create time or override per-press with --timeout.
	createCmd.Flags().IntVar(&createTimeout, "timeout", 300, "execution timeout in seconds")
	createCmd.Flags().StringVar(&createMaxResponseSize, "max-response-size", "", "max HTTP response body size for --url buttons (e.g. 10M, 1G). default: 10M")
	createCmd.Flags().BoolVar(&createAllowPrivateNetworks, "allow-private-networks", false, "allow --url buttons to reach private network addresses (localhost, 10/8, 172.16/12, 192.168/16, 169.254/16, IPv6 private ranges). Required for local dev targets.")
	createCmd.Flags().StringArrayVar(&createArgs, "arg", nil, "argument definition (name:type:required|optional)")
	createCmd.Flags().BoolVar(&createIgnore, "ignore", false, "add this button to .buttons/.gitignore so git won't track it (good for scratch/test buttons)")
	rootCmd.AddCommand(createCmd)
}
