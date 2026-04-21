package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/webhook"
)

// webhookCmd groups the three verbs a user needs to get webhook
// infrastructure working: `setup` (one-time CF login + named tunnel),
// `status` (what mode are we in / what's the public URL), and `test`
// (prove the loop works end-to-end).
var webhookCmd = &cobra.Command{
	Use:   "webhook",
	Short: "Expose a local URL via Cloudflare to receive webhook callbacks",
	Long: `Drawer steps occasionally need to register a public URL with a third-
party service (Apify, GitHub, Stripe) and wait for it to post back. The
webhook subsystem manages a Cloudflare tunnel to your local machine so
that URL works without hosting anything yourself.

Two modes:

  quick   zero setup. Each webhook step spawns a fresh Quick Tunnel
          (ephemeral *.trycloudflare.com URL). Good when the service
          accepts a per-run webhook URL.

  named   stable hostname on your own Cloudflare account. Set up once
          via 'buttons webhook setup'. Required when a service wants a
          fixed URL registered up front (e.g. GitHub webhooks).

Prereq: 'cloudflared' binary on PATH. Install via 'brew install cloudflared'.

Verbs:
  buttons webhook setup    — one-time: Cloudflare login + pick a hostname
  buttons webhook status   — show current mode + hostname
  buttons webhook test     — end-to-end round-trip verify
  buttons webhook listen   — run the dispatcher that presses triggered drawers
  buttons webhook logout   — forget the named-tunnel config (quick mode again)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWebhookStatus()
	},
}

var webhookSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "One-time Cloudflare login + named-tunnel config",
	Args:  cobra.NoArgs,
	RunE:  runWebhookSetup,
}

var webhookStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current webhook mode and URL",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWebhookStatus()
	},
}

var webhookTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Round-trip verify: spin up a tunnel, self-POST, observe delivery",
	Args:  cobra.NoArgs,
	RunE:  runWebhookTest,
}

var webhookLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Clear named-tunnel config (falls back to quick mode)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := webhook.DeleteConfig(); err != nil {
			return handleWebhookErr(err)
		}
		if jsonOutput {
			return config.WriteJSON(map[string]any{"mode": string(webhook.ModeQuick)})
		}
		fmt.Fprintln(os.Stderr, "Cleared named-tunnel config. Falling back to quick mode.")
		return nil
	},
}

// webhookSetupHostname is the --hostname override for non-interactive
// setup (CI, scripted installs). When empty the flow prompts.
var webhookSetupHostname string
var webhookSetupTunnelName string

// webhookSetupAllowApex is the explicit opt-in for apex-domain
// hostnames. Off by default because routing a user's apex (e.g.
// example.com) through the tunnel overrides every DNS record at the
// root — clobbers their website unless they knew what they were doing.
var webhookSetupAllowApex bool

// webhookSetupOverwriteDNS is the explicit opt-in for replacing a
// pre-existing Cloudflare DNS record at the target hostname. Off by
// default — setup normally surfaces DNS_CONFLICT so the user can
// delete the existing record manually before retrying.
var webhookSetupOverwriteDNS bool

// webhookSetupReLogin forces a fresh `cloudflared tunnel login`
// flow, bypassing the existing ~/.cloudflared/cert.pem. Use when the
// cert is bound to the wrong Cloudflare account/zone for the target
// hostname and the automatic zone-drift recovery (on ZoneMismatch)
// isn't kicking in, or when you want to explicitly re-pick accounts.
var webhookSetupReLogin bool

// webhookSetupAPIToken and webhookSetupAPIAccountID drive the
// --api-token code path: setup via CF REST API instead of cert.pem.
// Token permissions required: Account:Cloudflare Tunnel:Edit and
// Zone:DNS:Edit on the target zone(s). Also accepts the value from
// BUTTONS_CF_API_TOKEN so agents/CI never put the token in a
// process-list-visible argv.
var webhookSetupAPIToken string
var webhookSetupAPIAccountID string

// apexLikelyPublicSuffixes covers the common cases where a literal
// "ccTLD + 1" is still an apex that would break a real website. Not
// exhaustive (no PSL parse); the guardrail is best-effort — users who
// own something unusual can opt out via --allow-apex.
var apexLikelyPublicSuffixes = []string{
	".co.uk", ".co.nz", ".com.au", ".com.br", ".co.jp", ".co.in",
}

// validateWebhookHostname enforces the shape we expect for a webhook
// hostname: non-empty, dot-containing, and NOT an apex domain by our
// heuristic. Returns a helpful error pointing at the safe subdomain
// form when the user typed an apex.
func validateWebhookHostname(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return errors.New("hostname required")
	}
	if !strings.Contains(s, ".") {
		return errors.New("looks like a bare word — need a full domain like webhooks.example.com")
	}
	// Apex heuristic: count dots. "example.com" → 1 dot. A safe
	// subdomain like "webhooks.example.com" has 2+. ccTLD-style
	// apexes like "example.co.uk" also have 2 dots but end in a
	// known multi-part TLD — detect those separately.
	for _, psl := range apexLikelyPublicSuffixes {
		if strings.HasSuffix(s, psl) {
			// Remove the PSL suffix and count dots in the rest.
			rest := strings.TrimSuffix(s, psl)
			if !strings.Contains(rest, ".") {
				return apexHostnameError(s)
			}
			return nil
		}
	}
	if strings.Count(s, ".") < 2 {
		return apexHostnameError(s)
	}
	return nil
}

func apexHostnameError(s string) error {
	return fmt.Errorf(
		"hostname %q looks like an apex domain — routing the tunnel here will clobber every DNS record at the root (website, email, etc.). "+
			"Use a subdomain like webhooks.%s instead. If you really want the apex, re-run with --allow-apex",
		s, s,
	)
}

// runWebhookSetup walks the user through: binary check → cloudflared
// login if needed → hostname prompt → create + route tunnel → persist.
func runWebhookSetup(cmd *cobra.Command, args []string) error {
	if err := webhook.CheckCloudflared(); err != nil {
		return handleWebhookErr(err)
	}

	hostname := strings.TrimSpace(webhookSetupHostname)
	tunnelName := strings.TrimSpace(webhookSetupTunnelName)
	if tunnelName == "" {
		tunnelName = webhook.DefaultTunnelName
	}

	if hostname == "" {
		if jsonOutput || noInput || config.IsNonTTY() {
			return handleWebhookErr(errors.New("--hostname is required in non-interactive mode (example: --hostname webhooks.yourdomain.com)"))
		}
		if err := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Hostname to receive webhooks").
					Description("Must be on a zone managed by the Cloudflare account you're about to log in to.\n\nUse a SUBDOMAIN like webhooks.yourdomain.com — a bare apex (yourdomain.com) routes\nall root traffic through the tunnel and will clobber any existing website DNS.").
					Placeholder("webhooks.yourdomain.com").
					Value(&hostname).
					Validate(validateWebhookHostname),
			),
		).Run(); err != nil {
			if errors.Is(err, huh.ErrUserAborted) {
				return errSilent
			}
			return handleWebhookErr(err)
		}
		hostname = strings.TrimSpace(hostname)
	}
	// Non-interactive path runs the same validator so --hostname
	// autono.co (an apex) gets the same guardrail as the Huh prompt.
	if err := validateWebhookHostname(hostname); err != nil {
		if webhookSetupAllowApex && strings.Contains(err.Error(), "apex") {
			// Opt-in override for users who genuinely want the
			// tunnel at their apex (e.g. they own the domain solely
			// for webhook routing). Log prominently.
			fmt.Fprintf(os.Stderr, "WARNING: proceeding with apex hostname %q — this will clobber any existing DNS at %s. --allow-apex was set.\n", hostname, hostname)
		} else {
			return handleWebhookErr(err)
		}
	}

	// Login inherits the user's terminal so the browser prompt appears.
	// SkipLogin piggybacks on cert.pem presence — the second run of
	// setup (to change hostname) doesn't re-open the browser.
	ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Minute)
	defer cancel()

	// Resolve API token (here instead of near the RunSetup call so
	// the banner can mention it). --api-token flag wins; otherwise
	// pick up $BUTTONS_CF_API_TOKEN so secrets never appear in argv.
	apiToken := webhookSetupAPIToken
	if apiToken == "" {
		apiToken = os.Getenv("BUTTONS_CF_API_TOKEN")
	}

	if !jsonOutput {
		switch {
		case apiToken != "":
			fmt.Fprintln(os.Stderr, "  Using Cloudflare API token (skipping cert.pem flow).")
		case webhook.HasCloudflaredCert():
			fmt.Fprintln(os.Stderr, "  Using existing Cloudflare login (~/.cloudflared/cert.pem).")
		default:
			fmt.Fprintln(os.Stderr, "  Opening browser for Cloudflare login…")
		}
	}

	result, err := webhook.RunSetup(ctx, webhook.SetupOpts{
		Hostname:     hostname,
		TunnelName:   tunnelName,
		OverwriteDNS: webhookSetupOverwriteDNS,
		ForceLogin:   webhookSetupReLogin,
		APIToken:     apiToken,
		APIAccountID: webhookSetupAPIAccountID,
	})
	if err != nil {
		// DNS conflict gets its own error code so agents/scripts can
		// distinguish it from generic setup failures and retry with
		// --overwrite-dns after checking the existing record.
		var dnsErr *webhook.DNSConflictError
		if errors.As(err, &dnsErr) {
			if jsonOutput {
				_ = config.WriteJSONError("DNS_CONFLICT", err.Error())
				return errSilent
			}
			fmt.Fprintln(os.Stderr, err.Error())
			return errSilent
		}
		var zmErr *webhook.ZoneMismatchError
		if errors.As(err, &zmErr) {
			if jsonOutput {
				_ = config.WriteJSONError("ZONE_MISMATCH", err.Error())
				return errSilent
			}
			fmt.Fprintln(os.Stderr, err.Error())
			fmt.Fprintln(os.Stderr, "\nTry: /tmp/buttons webhook setup --hostname <HOST> --re-login")
			return errSilent
		}
		return handleWebhookErr(err)
	}

	if jsonOutput {
		return config.WriteJSON(map[string]any{
			"mode":        string(webhook.ModeNamed),
			"hostname":    result.Hostname,
			"tunnel_name": result.TunnelName,
			"tunnel_id":   result.TunnelID,
		})
	}
	fmt.Fprintf(os.Stderr, "\nConnected.\n")
	fmt.Fprintf(os.Stderr, "  Hostname:    https://%s\n", result.Hostname)
	fmt.Fprintf(os.Stderr, "  Tunnel:      %s (%s)\n", result.TunnelName, result.TunnelID)
	printNextHint("buttons webhook test")
	return nil
}

func runWebhookStatus() error {
	cfg, err := webhook.LoadConfig()
	if err != nil {
		return handleWebhookErr(err)
	}
	mode := webhook.ModeQuick
	var hostname, tunnelName, tunnelID string
	if cfg != nil && cfg.Mode == webhook.ModeNamed {
		mode = webhook.ModeNamed
		hostname = cfg.Hostname
		tunnelName = cfg.TunnelName
		tunnelID = cfg.TunnelID
	}
	binOK := webhook.CheckCloudflared() == nil

	if jsonOutput {
		return config.WriteJSON(map[string]any{
			"mode":            string(mode),
			"hostname":        hostname,
			"tunnel_name":     tunnelName,
			"tunnel_id":       tunnelID,
			"cloudflared_ok":  binOK,
		})
	}
	fmt.Fprintf(os.Stderr, "  mode:         %s\n", mode)
	if mode == webhook.ModeNamed {
		fmt.Fprintf(os.Stderr, "  hostname:     https://%s\n", hostname)
		fmt.Fprintf(os.Stderr, "  tunnel:       %s (%s)\n", tunnelName, tunnelID)
	} else {
		fmt.Fprintf(os.Stderr, "  hostname:     (ephemeral trycloudflare.com per run)\n")
	}
	if binOK {
		fmt.Fprintf(os.Stderr, "  cloudflared:  installed\n")
	} else {
		fmt.Fprintf(os.Stderr, "  cloudflared:  MISSING — brew install cloudflared\n")
	}
	if mode == webhook.ModeQuick {
		printNextHint("buttons webhook setup  — upgrade to a stable hostname")
	}
	return nil
}

// runWebhookTest proves the whole loop: listener → tunnel → public URL
// → POST back → delivery observed. Takes a minute end-to-end because
// cloudflared needs time to register a connection.
func runWebhookTest(cmd *cobra.Command, args []string) error {
	if err := webhook.CheckCloudflared(); err != nil {
		return handleWebhookErr(err)
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 90*time.Second)
	defer cancel()

	// Ctrl-C during test should tear down cleanly.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	srv, err := webhook.NewServer()
	if err != nil {
		return handleWebhookErr(err)
	}
	defer func() { _ = srv.Close() }()

	if !jsonOutput {
		fmt.Fprintf(os.Stderr, "  Starting tunnel (this takes ~15s)…\n")
	}
	t, err := webhook.StartTunnel(ctx, srv.LocalURL(), srv.ReadyToken())
	if err != nil {
		return handleWebhookErr(err)
	}
	defer func() { _ = t.Stop() }()

	corr, err := webhook.NewCorrelationID()
	if err != nil {
		return handleWebhookErr(err)
	}
	// Register the waiter BEFORE publishing the URL (spawning the
	// POST goroutine). Eliminates the "event arrives before Wait
	// registers" race — no pending-map needed server-side.
	waiterCh := srv.Register(corr)
	defer srv.Deregister(corr)

	postURL := fmt.Sprintf("%s/webhook/%s", t.URL, corr)

	if !jsonOutput {
		fmt.Fprintf(os.Stderr, "  Tunnel up: %s\n", t.URL)
		fmt.Fprintf(os.Stderr, "  POSTing to %s …\n", postURL)
	}

	// Fire the test POST in the background. Tunnel readiness is
	// already verified by StartTunnel's /healthz + token check.
	delivery := make(chan error, 1)
	go func() {
		delivery <- selfPost(ctx, postURL)
	}()

	waitCtx, waitCancel := context.WithTimeout(ctx, 60*time.Second)
	defer waitCancel()

	var ev webhook.Event
	var waitErr error
	select {
	case ev = <-waiterCh:
	case <-waitCtx.Done():
		waitErr = waitCtx.Err()
	}
	if waitErr != nil {
		postErr := <-delivery
		return handleWebhookErr(fmt.Errorf("no webhook received within 60s (post err: %v, wait err: %w)", postErr, waitErr))
	}
	if postErr := <-delivery; postErr != nil {
		// Unusual: server received the event but the POST goroutine
		// reported an error. Surface it but don't fail the test —
		// delivery won.
		if !jsonOutput {
			fmt.Fprintf(os.Stderr, "  (note: POST goroutine reported %v)\n", postErr)
		}
	}

	if jsonOutput {
		return config.WriteJSON(map[string]any{
			"mode":         string(t.Mode),
			"url":          t.URL,
			"correlation":  corr,
			"received_at":  ev.ReceivedAt,
			"body":         ev.Body,
		})
	}
	fmt.Fprintf(os.Stderr, "\nWebhook round-trip ok (%s mode).\n", t.Mode)
	fmt.Fprintf(os.Stderr, "  received: %s\n", ev.ReceivedAt.Format(time.RFC3339))
	return nil
}

// selfPost fires one test request at the public URL. The tunnel
// manager already waited for /healthz to respond before handing us the
// URL, so a single POST is sufficient — retries here would only mask a
// real problem.
func selfPost(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(`{"hello":"from buttons webhook test"}`))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status %d from test POST", resp.StatusCode)
	}
	return nil
}

// handleWebhookErr renders cloudflared-missing errors cleanly and
// everything else via the standard JSON/stderr split.
func handleWebhookErr(err error) error {
	if err == nil {
		return nil
	}
	var missing webhook.CloudflaredMissingError
	if errors.As(err, &missing) {
		if jsonOutput {
			_ = config.WriteJSONError("CLOUDFLARED_MISSING", err.Error())
			return errSilent
		}
		fmt.Fprintln(os.Stderr, err.Error())
		return errSilent
	}
	if jsonOutput {
		_ = config.WriteJSONError("WEBHOOK_ERROR", err.Error())
		return errSilent
	}
	fmt.Fprintln(os.Stderr, "webhook:", err.Error())
	return errSilent
}

func init() {
	rootCmd.AddCommand(webhookCmd)
	webhookCmd.AddCommand(webhookSetupCmd)
	webhookCmd.AddCommand(webhookStatusCmd)
	webhookCmd.AddCommand(webhookTestCmd)
	webhookCmd.AddCommand(webhookLogoutCmd)
	webhookCmd.AddCommand(webhookListenCmd)

	webhookSetupCmd.Flags().StringVar(&webhookSetupHostname, "hostname", "", "hostname for webhooks (e.g. webhooks.yourdomain.com)")
	webhookSetupCmd.Flags().StringVar(&webhookSetupTunnelName, "tunnel", webhook.DefaultTunnelName, "Cloudflare tunnel name")
	webhookSetupCmd.Flags().BoolVar(&webhookSetupAllowApex, "allow-apex", false, "allow an apex hostname (e.g. example.com); DANGEROUS — overrides root DNS")
	webhookSetupCmd.Flags().BoolVar(&webhookSetupOverwriteDNS, "overwrite-dns", false, "replace any pre-existing Cloudflare DNS record at the hostname; DESTRUCTIVE")
	webhookSetupCmd.Flags().BoolVar(&webhookSetupReLogin, "re-login", false, "force fresh `cloudflared tunnel login`; use when the current cert.pem is bound to the wrong CF account")
	webhookSetupCmd.Flags().StringVar(&webhookSetupAPIToken, "api-token", "", "Cloudflare API token (skips cert.pem flow; or set $BUTTONS_CF_API_TOKEN)")
	webhookSetupCmd.Flags().StringVar(&webhookSetupAPIAccountID, "api-account-id", "", "pin the CF account id when the token is authorized on several")
}
