package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-isatty"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/config"
	"github.com/spf13/cobra"
)

var createFile string
var createCode string
var createCodeStdin bool
var createRuntime string
var createURL string
var createMethod string
var createHeaders []string
var createBody string
var createAgent string
var createDescription string
var createTimeout int
var createMaxResponseSize string
var createAllowPrivateNetworks bool
var createArgs []string

var createCmd = &cobra.Command{
	Use:   "create [name]",
	Short: "Create a new button",
	Long: `Create a new button from a script file, inline code, or API endpoint.

A button wraps a single action with typed arguments, a timeout, and
structured output. Provide --file for a script, --code for inline code,
or --url for an HTTP API endpoint.

Arguments are defined with --arg in name:type:required|optional format.
Supported types: string, int, bool. Args are injected as env vars for
scripts or substituted into URL templates for API buttons.

Examples:
  buttons create deploy -f ./scripts/deploy.sh --arg env:string:required
  buttons create greet --code 'echo "Hello, $BUTTONS_ARG_NAME"' --arg name:string:required
  buttons create weather --url 'https://wttr.in/{{city}}?format=j1' --arg city:string:required
  buttons create webhook --url https://api.example.com/hook --method POST
  buttons create graphql --url https://api.example.com/graphql --method POST \
    --header "Content-Type: application/json" --body '{"query": "{ viewer { login } }"}'
  buttons create check-logs --agent "Use the Northflank CLI to read production logs and summarize errors"`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		code := createCode

		// Read code from stdin if --code-stdin
		if createCodeStdin {
			if createFile != "" {
				return handleServiceError(&button.ServiceError{Code: "VALIDATION_ERROR", Message: "cannot use both --file and --code-stdin"})
			}
			if createCode != "" {
				return handleServiceError(&button.ServiceError{Code: "VALIDATION_ERROR", Message: "cannot use both --code and --code-stdin"})
			}
			if isatty.IsTerminal(os.Stdin.Fd()) || isatty.IsCygwinTerminal(os.Stdin.Fd()) {
				return handleServiceError(&button.ServiceError{Code: "VALIDATION_ERROR", Message: "--code-stdin requires piped input (stdin is a terminal)"})
			}
			data, err := io.ReadAll(io.LimitReader(os.Stdin, 65536+1))
			if err != nil {
				return handleServiceError(&button.ServiceError{Code: "VALIDATION_ERROR", Message: fmt.Sprintf("failed to read stdin: %v", err)})
			}
			if len(data) > 65536 {
				return handleServiceError(&button.ServiceError{Code: "VALIDATION_ERROR", Message: "stdin code exceeds 64KB limit"})
			}
			if len(data) == 0 {
				return handleServiceError(&button.ServiceError{Code: "VALIDATION_ERROR", Message: "no code provided on stdin"})
			}
			code = string(data)
		}

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
			Agent:                createAgent,
			Description:          createDescription,
			TimeoutSeconds:       createTimeout,
			MaxResponseBytes:     maxResponseBytes,
			AllowPrivateNetworks: createAllowPrivateNetworks,
			Args:                 argDefs,
		})
		if err != nil {
			return handleServiceError(err)
		}

		if jsonOutput {
			return config.WriteJSON(btn)
		}

		fmt.Fprintf(os.Stderr, "Created button: %s\n", btn.Name)
		return nil
	},
}

func init() {
	createCmd.Flags().StringVarP(&createFile, "file", "f", "", "path to script file")
	createCmd.Flags().StringVar(&createCode, "code", "", "inline script code")
	createCmd.Flags().BoolVar(&createCodeStdin, "code-stdin", false, "read code from stdin")
	createCmd.Flags().StringVar(&createRuntime, "runtime", "", "code runtime: shell, python, node (default: shell)")
	createCmd.Flags().StringVar(&createURL, "url", "", "HTTP API endpoint URL (supports {{arg}} templates)")
	createCmd.Flags().StringVar(&createMethod, "method", "", "HTTP method for --url (default: GET)")
	createCmd.Flags().StringArrayVar(&createHeaders, "header", nil, "HTTP header as 'Key: Value' (repeatable)")
	createCmd.Flags().StringVar(&createBody, "body", "", "HTTP request body (supports {{arg}} templates)")
	createCmd.Flags().StringVar(&createAgent, "agent", "", "agent instruction/system prompt")
	createCmd.Flags().StringVarP(&createDescription, "description", "d", "", "button description")
	createCmd.Flags().IntVar(&createTimeout, "timeout", 60, "execution timeout in seconds")
	createCmd.Flags().StringVar(&createMaxResponseSize, "max-response-size", "", "max HTTP response body size for --url buttons (e.g. 10M, 1G). default: 10M")
	createCmd.Flags().BoolVar(&createAllowPrivateNetworks, "allow-private-networks", false, "allow --url buttons to reach private network addresses (localhost, 10/8, 172.16/12, 192.168/16, 169.254/16, IPv6 private ranges). Required for local dev targets.")
	createCmd.Flags().StringArrayVar(&createArgs, "arg", nil, "argument definition (name:type:required|optional)")
	rootCmd.AddCommand(createCmd)
}
