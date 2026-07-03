package cmd

import (
	"fmt"
	"os"
	"regexp"
	"runtime"

	"github.com/autonoco/buttons/internal/agent"
	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/webhook"
	"github.com/spf13/cobra"
)

// slugRe mirrors the broker's hostname-label rule: one DNS label, so an invalid
// slug is rejected locally with a clear message instead of a round-trip + 400.
var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

var (
	agentSetupTunnel    string
	agentSetupPrincipal string
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Set up this agent's identity and public URL",
	Long: `Set up and inspect this agent workspace's identity and public URL.

The device holds an Ed25519 keypair generated on first use and stored 0600 in the
data dir (agent.json); the private key never leaves the machine. Identity is
proven by signature, not asserted.

Uses $BUTTONS_REGISTRY_URL as the registry base URL (this repo ships no default
host). First-time setup consumes a one-time token from the ENROLL_TOKEN battery
(or $BUTTONS_BAT_ENROLL_TOKEN).`,
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

var agentSetupCmd = &cobra.Command{
	Use:   "setup <slug>",
	Short: "Register this agent under a slug and print its URLs (enrolls on first run)",
	Long: `Set up this agent's identity and public URL. One idempotent command:
generates the device key on first use, enrolls with the ENROLL_TOKEN battery if
the device isn't bound yet, then registers the slug and prints its URLs. Safe to
re-run — it re-points to the current tunnel.

--tunnel defaults to the tunnel id from a configured named webhook tunnel
(buttons webhook setup) when present.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		slug := args[0]
		reg := registryURL()
		if reg == "" {
			return agentConfigError("no registry target: set $BUTTONS_REGISTRY_URL")
		}
		if !slugRe.MatchString(slug) {
			return agentConfigError("slug must be one DNS label: lowercase letters, digits and hyphens, starting alphanumeric (max 63 chars)")
		}
		tunnel := agentSetupTunnel
		if tunnel == "" {
			if wc, err := webhook.LoadConfig(); err == nil && wc != nil {
				tunnel = wc.TunnelID
			}
		}
		if tunnel == "" {
			return agentConfigError("no tunnel id: pass --tunnel <id> (or configure a named tunnel with `buttons webhook setup`)")
		}
		c, err := agent.LoadOrCreate()
		if err != nil {
			return agentRuntimeError("AGENT_KEY_ERROR", err)
		}
		id, err := c.Identity()
		if err != nil {
			return agentRuntimeError("AGENT_KEY_ERROR", err)
		}

		token := enrollToken()
		res, err := (&agent.Client{BaseURL: reg}).Setup(cmd.Context(), id, token, runtime.GOOS+"/"+runtime.GOARCH, agent.RegisterParams{
			Slug:      slug,
			TunnelID:  tunnel,
			Principal: agentSetupPrincipal,
		})
		if agent.IsNotEnrolled(err) && token == "" {
			// Only the no-token case gets the setup hint; a not-enrolled error DESPITE a
			// supplied token is a real failure → fall through to SETUP_ERROR.
			return agentConfigError("device not enrolled and no ENROLL_TOKEN set: run `buttons batteries set ENROLL_TOKEN <token>` (or set $BUTTONS_BAT_ENROLL_TOKEN)")
		}
		if err != nil {
			return agentRuntimeError("SETUP_ERROR", err)
		}

		// Persist the slug locally so `status` and re-runs know it.
		c.Slug = res.Slug
		if err := agent.SaveConfig(c); err != nil {
			return agentRuntimeError("AGENT_KEY_ERROR", err)
		}

		if jsonOutput {
			return config.WriteJSON(res)
		}
		fmt.Fprintf(os.Stderr, "Agent %s is set up (%s)\n", res.Slug, res.Status)
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
			fmt.Fprintln(os.Stderr, "not set up — run `buttons agent setup <slug>`")
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
	agentSetupCmd.Flags().StringVar(&agentSetupTunnel, "tunnel", "", "tunnel id backing this agent (defaults to the named webhook tunnel)")
	agentSetupCmd.Flags().StringVar(&agentSetupPrincipal, "principal", "", "optional principal this agent serves")
	agentCmd.AddCommand(agentSetupCmd, agentStatusCmd)
	rootCmd.AddCommand(agentCmd)
}
