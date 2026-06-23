package cmd

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/autonoco/buttons/internal/mcpserver"
	"github.com/spf13/cobra"
)

var mcpAllowCreate bool

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Run an MCP server over stdio (expose buttons to agents)",
	Long: `Start a Model Context Protocol server on stdio so an agent (e.g. Claude
Code) can discover and press buttons as tools.

Uses a thin meta-tool surface — buttons_list, buttons_press, buttons_inspect
(and buttons_create with --allow-create) — instead of one tool per button, so
large button sets don't degrade the MCP client.

Security:
  - Only buttons with "mcp_enabled": true are listed, pressable, or inspectable.
  - Args are validated against the button's spec and passed as BUTTONS_ARG_<NAME>
    env vars — never substituted into shell text.
  - Per button: max 10 calls/min, 1 concurrent press, hard 120s timeout cap.
  - buttons_create is OFF unless --allow-create is passed.

stdout carries only protocol messages; logs go to stderr. Register with an
agent, e.g. Claude Code:
  claude mcp add buttons -- buttons mcp`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		srv := mcpserver.New(mcpserver.Config{
			AllowCreate: mcpAllowCreate,
			Version:     version,
		})
		ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		return srv.Serve(ctx, os.Stdin, os.Stdout)
	},
}

func init() {
	mcpCmd.Flags().BoolVar(&mcpAllowCreate, "allow-create", false, "expose the buttons_create tool (off by default)")
	rootCmd.AddCommand(mcpCmd)
}
