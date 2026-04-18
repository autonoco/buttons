package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/config"
	"github.com/spf13/cobra"
)

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
Supported types: string, int, bool. Args are injected as env vars for
scripts or substituted into URL templates for API buttons.

Common flags:
  -f, --file PATH       copy an existing script file as this button's code
      --code STRING     inline script body (shortcut for one-liners)
      --url URL         turn this into an HTTP button
      --arg SPEC        define an arg (name:type:required|optional, repeatable)
      --timeout SECS    execution timeout (default: 300)
  -d, --description S   human-readable description for 'buttons list'
      --runtime NAME    shell | python | node  (default: shell)

Examples:
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
			TimeoutSeconds:       createTimeout,
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

		if jsonOutput {
			return config.WriteJSON(struct {
				*button.Button
				CodePath  string `json:"code_path,omitempty"`
				ButtonDir string `json:"button_dir"`
			}{
				Button:    btn,
				CodePath:  codePath,
				ButtonDir: btnDir,
			})
		}

		fmt.Fprintf(os.Stderr, "Created button: %s\n", btn.Name)
		if codePath != "" {
			fmt.Fprintf(os.Stderr, "  Edit:  %s\n", codePath)
		} else if btn.Runtime == "prompt" {
			fmt.Fprintf(os.Stderr, "  Edit:  %s\n", filepath.Join(btnDir, "AGENT.md"))
		}
		fmt.Fprintf(os.Stderr, "  Press: buttons press %s\n", btn.Name)
		return nil
	},
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
	rootCmd.AddCommand(createCmd)
}
