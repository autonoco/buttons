package cmd

import (
	"fmt"
	"os"
	"runtime"

	"github.com/autonoco/buttons/internal/agent"
	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/webhook"
	"github.com/spf13/cobra"
)

var (
	agentRegisterSlug      string
	agentRegisterTunnel    string
	agentRegisterAgentID   string
	agentRegisterPrincipal string
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Register this workspace's device identity with the registry",
	Long: `Manage this agent workspace's device identity.

The device holds an Ed25519 keypair generated on first use and stored 0600 in the
data dir (agent.json); the private key never leaves the machine. Identity is
proven by signature, not asserted.

All subcommands use $BUTTONS_REGISTRY_URL as the registry base URL — this repo
ships no default host. Enrollment consumes a one-time token supplied as the
ENROLL_TOKEN battery (or $BUTTONS_BAT_ENROLL_TOKEN).`,
	RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
}

// enrollToken resolves the one-time enrollment token — the injected
// BUTTONS_BAT_ENROLL_TOKEN env wins, else the ENROLL_TOKEN battery.
func enrollToken() string {
	if t := os.Getenv("BUTTONS_BAT_ENROLL_TOKEN"); t != "" {
		return t
	}
	if svc, err := newBatteryService(); err == nil {
		if v, _, err := svc.Get("ENROLL_TOKEN"); err == nil {
			return v
		}
	}
	return ""
}

// agentConfigError renders a pre-flight validation error in the active format.
func agentConfigError(msg string) error {
	if jsonOutput {
		_ = config.WriteJSONError("VALIDATION_ERROR", msg)
		return errSilent
	}
	return fmt.Errorf("%s", msg)
}

func agentRuntimeError(code string, err error) error {
	if jsonOutput {
		_ = config.WriteJSONError(code, err.Error())
		return errSilent
	}
	return err
}

var agentEnrollCmd = &cobra.Command{
	Use:   "enroll",
	Short: "Generate this device's key and bind it to its owner with a one-time token",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, _ []string) error {
		reg := registryURL()
		if reg == "" {
			return agentConfigError("no registry target: set $BUTTONS_REGISTRY_URL")
		}
		token := enrollToken()
		if token == "" {
			return agentConfigError("no enroll token: run `buttons batteries set ENROLL_TOKEN <token>` (or set $BUTTONS_BAT_ENROLL_TOKEN)")
		}
		c, err := agent.LoadOrCreate()
		if err != nil {
			return agentRuntimeError("AGENT_KEY_ERROR", err)
		}
		id, err := c.Identity()
		if err != nil {
			return agentRuntimeError("AGENT_KEY_ERROR", err)
		}
		res, err := (&agent.Client{BaseURL: reg}).Enroll(cmd.Context(), token, runtime.GOOS+"/"+runtime.GOARCH, id)
		if err != nil {
			return agentRuntimeError("ENROLL_ERROR", err)
		}
		if jsonOutput {
			return config.WriteJSON(res)
		}
		fmt.Fprintf(os.Stderr, "Enrolled device %s (owner %s)\n", res.DeviceID, res.OwnerID)
		printNextHint("buttons agent register --slug <name> --tunnel <tunnel-id>")
		return nil
	},
}

var agentRegisterCmd = &cobra.Command{
	Use:   "register",
	Short: "Register this workspace under a slug and print its URLs",
	Long: `Register this workspace: claim a slug, and receive its URL set from the
registry. Requires an enrolled device (run "buttons agent enroll" first).

--tunnel defaults to the tunnel id from a configured named webhook tunnel
(buttons webhook setup) when present.`,
	Args: exactArgs(0),
	RunE: func(cmd *cobra.Command, _ []string) error {
		reg := registryURL()
		if reg == "" {
			return agentConfigError("no registry target: set $BUTTONS_REGISTRY_URL")
		}
		if agentRegisterSlug == "" {
			return agentConfigError("--slug is required")
		}
		c, err := agent.LoadConfig()
		if err != nil {
			return agentRuntimeError("AGENT_KEY_ERROR", err)
		}
		if c == nil || c.DeviceSeed == "" {
			return agentConfigError("device not enrolled: run `buttons agent enroll` first")
		}
		id, err := c.Identity()
		if err != nil {
			return agentRuntimeError("AGENT_KEY_ERROR", err)
		}

		tunnel := agentRegisterTunnel
		if tunnel == "" {
			if wc, err := webhook.LoadConfig(); err == nil && wc != nil {
				tunnel = wc.TunnelID
			}
		}
		if tunnel == "" {
			return agentConfigError("no tunnel id: pass --tunnel <id> (or configure a named tunnel with `buttons webhook setup`)")
		}

		res, err := (&agent.Client{BaseURL: reg}).Register(cmd.Context(), id, agent.RegisterParams{
			Slug:      agentRegisterSlug,
			TunnelID:  tunnel,
			AgentID:   agentRegisterAgentID,
			Principal: agentRegisterPrincipal,
		})
		if err != nil {
			return agentRuntimeError("REGISTER_ERROR", err)
		}

		// Persist the slug locally so `status` and re-registers know it.
		c.Slug = res.Slug
		if err := agent.SaveConfig(c); err != nil {
			return agentRuntimeError("AGENT_KEY_ERROR", err)
		}

		if jsonOutput {
			return config.WriteJSON(res)
		}
		fmt.Fprintf(os.Stderr, "Registered %s (%s)\n", res.Slug, res.Status)
		fmt.Fprintf(os.Stderr, "  webhook: %s\n", res.URLs.Webhook)
		fmt.Fprintf(os.Stderr, "  tunnel:  %s\n", res.URLs.Tunnel)
		fmt.Fprintf(os.Stderr, "  wake:    %s\n", res.URLs.Wake)
		if res.URLs.Deploy != nil {
			fmt.Fprintf(os.Stderr, "  deploy:  %s\n", *res.URLs.Deploy)
		}
		return nil
	},
}

var agentStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show this device's identity (no secrets)",
	Args:  exactArgs(0),
	RunE: func(_ *cobra.Command, _ []string) error {
		c, err := agent.LoadConfig()
		if err != nil {
			return agentRuntimeError("AGENT_KEY_ERROR", err)
		}
		if c == nil || c.DeviceSeed == "" {
			if jsonOutput {
				return config.WriteJSON(map[string]any{"enrolled": false})
			}
			fmt.Fprintln(os.Stderr, "not enrolled — run `buttons agent enroll`")
			return nil
		}
		id, err := c.Identity()
		if err != nil {
			return agentRuntimeError("AGENT_KEY_ERROR", err)
		}
		if jsonOutput {
			return config.WriteJSON(map[string]any{"enrolled": true, "device_id": id.DeviceID, "slug": c.Slug})
		}
		fmt.Fprintf(os.Stderr, "device %s\n", id.DeviceID)
		if c.Slug != "" {
			fmt.Fprintf(os.Stderr, "slug   %s\n", c.Slug)
		}
		return nil
	},
}

func init() {
	agentRegisterCmd.Flags().StringVar(&agentRegisterSlug, "slug", "", "the workspace slug to claim (one DNS label)")
	agentRegisterCmd.Flags().StringVar(&agentRegisterTunnel, "tunnel", "", "tunnel id backing this workspace (defaults to the named webhook tunnel)")
	agentRegisterCmd.Flags().StringVar(&agentRegisterAgentID, "agent-id", "", "optional persona id")
	agentRegisterCmd.Flags().StringVar(&agentRegisterPrincipal, "principal", "", "optional principal this workspace serves")
	agentCmd.AddCommand(agentEnrollCmd, agentRegisterCmd, agentStatusCmd)
	rootCmd.AddCommand(agentCmd)
}
