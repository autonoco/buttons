package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// DefaultTunnelName is used when the user doesn't override via
// SetupOpts.TunnelName. "buttons" is memorable and unambiguous in the
// Cloudflare dashboard.
const DefaultTunnelName = "buttons"

// SetupOpts configures the one-time named-tunnel setup flow. Leave
// fields empty to accept defaults / fall back to interactive prompts in
// the CLI wrapper.
type SetupOpts struct {
	Hostname   string // e.g. "webhooks.autono.dev"
	TunnelName string // defaults to DefaultTunnelName
	// SkipLogin assumes `cloudflared tunnel login` has already been run
	// on this machine. Used by the CLI after it confirms ~/.cloudflared/cert.pem exists.
	SkipLogin bool
	// OverwriteDNS authorises routeDNS to pass --overwrite-dns to
	// cloudflared, replacing any pre-existing record at Hostname.
	// Off by default — the safe fallback is to surface a
	// DNSConflictError and let the user decide.
	OverwriteDNS bool
	// ForceLogin wipes ~/.cloudflared/cert.pem and re-runs `tunnel
	// login` so the user can pick a different Cloudflare account.
	// Matches the CLI's --re-login flag.
	ForceLogin bool
	// NoAutoRelogin disables the implicit re-auth path that fires on
	// ZoneMismatchError. CLI / test convenience; production callers
	// leave this false so zone-drift self-heals.
	NoAutoRelogin bool
	// APIToken, when non-empty, skips the cloudflared-based flow
	// entirely and uses the CF REST API: list accounts, resolve
	// zone, create tunnel, mint token, create DNS. Multi-zone
	// capable, headless-friendly, no browser.
	APIToken string
	// APIAccountID optionally pins a specific CF account when the
	// token is authorized on several. Empty = auto-pick if a single
	// account is authorized, error out otherwise (caller prompts).
	APIAccountID string
}

// SetupResult reports what got persisted so the CLI can render a clean
// summary.
type SetupResult struct {
	Hostname   string `json:"hostname"`
	TunnelName string `json:"tunnel_name"`
	TunnelID   string `json:"tunnel_id"`
}

// CertPath is where cloudflared stores the CA cert after `tunnel login`.
// We never read it — we only check existence so the CLI can decide
// whether to trigger the login step.
const CertPath = "cert.pem"

// resetCloudflaredCert deletes ~/.cloudflared/cert.pem so a subsequent
// Login() re-opens the browser and lets the user pick a different
// Cloudflare account. Missing file is not an error — target state is
// "no cert" either way.
func resetCloudflaredCert() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := fmt.Sprintf("%s/.cloudflared/%s", home, CertPath)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// HasCloudflaredCert returns true when ~/.cloudflared/cert.pem exists,
// which is cloudflared's indicator that `tunnel login` has run.
func HasCloudflaredCert() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	_, err = os.Stat(fmt.Sprintf("%s/.cloudflared/%s", home, CertPath))
	return err == nil
}

// Login runs `cloudflared tunnel login`, which opens a browser so the
// user authorizes a Cloudflare account + zone. Returns once cert.pem is
// on disk or ctx fires.
func Login(ctx context.Context) error {
	if err := CheckCloudflared(); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "cloudflared", "tunnel", "login") // #nosec G204 -- literal args
	cmd.Stdout = os.Stderr // cloudflared prints the auth URL; show on TTY
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cloudflared tunnel login: %w", err)
	}
	if !HasCloudflaredCert() {
		return errors.New("cloudflared login completed but ~/.cloudflared/cert.pem missing")
	}
	return nil
}

// tunnelIDPattern picks the tunnel UUID out of `cloudflared tunnel
// create` output. The line looks roughly like:
//   Created tunnel buttons with id 1234abcd-...
var tunnelIDPattern = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// createOrFindTunnel creates a named tunnel or returns the existing
// tunnel's UUID if it already exists. Idempotent so re-running setup is
// safe.
func createOrFindTunnel(ctx context.Context, name string) (string, error) {
	// Try list first — faster path if the tunnel already exists.
	if id, err := findTunnelID(ctx, name); err == nil && id != "" {
		return id, nil
	}
	cmd := exec.CommandContext(ctx, "cloudflared", "tunnel", "create", name) // #nosec G204
	out, err := cmd.CombinedOutput()
	if err != nil {
		// "already exists" path — ask list again.
		if strings.Contains(string(out), "already exists") {
			if id, e2 := findTunnelID(ctx, name); e2 == nil && id != "" {
				return id, nil
			}
		}
		return "", fmt.Errorf("cloudflared tunnel create %s: %w\n%s", name, err, string(out))
	}
	if m := tunnelIDPattern.FindString(string(out)); m != "" {
		return m, nil
	}
	// Last resort: list.
	if id, err := findTunnelID(ctx, name); err == nil && id != "" {
		return id, nil
	}
	return "", fmt.Errorf("could not determine tunnel id for %s from:\n%s", name, string(out))
}

// findTunnelID shells out to `cloudflared tunnel list --output json` and
// picks the first tunnel matching `name`. `--output json` has been
// stable in cloudflared for years.
func findTunnelID(ctx context.Context, name string) (string, error) {
	cmd := exec.CommandContext(ctx, "cloudflared", "tunnel", "list", "--output", "json") // #nosec G204
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	var tunnels []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(out, &tunnels); err != nil {
		return "", fmt.Errorf("parse tunnel list: %w", err)
	}
	for _, t := range tunnels {
		if t.Name == name {
			return t.ID, nil
		}
	}
	return "", nil
}

// DNSConflictError is returned by routeDNS when Cloudflare already has
// a record at the target hostname that's not the tunnel's. Callers
// surface this to the user verbatim so they can decide whether to
// delete the existing record (safe) or re-run setup with
// --overwrite-dns (destructive).
type DNSConflictError struct {
	Hostname   string
	TunnelName string
	Raw        string // stderr from cloudflared for diagnostics
}

func (e *DNSConflictError) Error() string {
	return fmt.Sprintf(
		"a DNS record already exists at %s and is not pointing at tunnel %q. "+
			"Setup refuses to overwrite unowned records.\n\n"+
			"To proceed, either:\n"+
			"  • delete the existing record in your Cloudflare dashboard, then re-run setup, or\n"+
			"  • re-run setup with --overwrite-dns to replace it (DESTRUCTIVE — point-of-no-return)\n\n"+
			"cloudflared output:\n%s",
		e.Hostname, e.TunnelName, e.Raw,
	)
}

// routeDNS attaches the hostname to the tunnel. By default it refuses
// to overwrite a pre-existing Cloudflare DNS record — surfaces a
// DNSConflictError so the CLI can print actionable remediation. Pass
// overwrite=true to run with --overwrite-dns (destructive).
//
// The idempotent "setup twice with the same hostname and tunnel"
// case is handled correctly without --overwrite: cloudflared's
// "already exists" error fires in both the "record points at us
// already" and "record belongs to someone else" cases, and we can't
// tell them apart from the exit code alone. We conservatively treat
// ALL already-exists responses as a conflict — that's the safer
// default. Users re-running setup after a successful initial run
// should see the error and either confirm with --overwrite-dns or
// recognise their config is already good and move on.
func routeDNS(ctx context.Context, tunnelName, hostname string, overwrite bool) error {
	args := []string{"tunnel", "route", "dns"}
	if overwrite {
		args = append(args, "--overwrite-dns")
	}
	args = append(args, tunnelName, hostname)
	cmd := exec.CommandContext(ctx, "cloudflared", args...) // #nosec G204 -- static flags + validated tunnelName/hostname
	out, err := cmd.CombinedOutput()
	combined := string(out)
	if err != nil {
		if strings.Contains(combined, "already exists") || strings.Contains(combined, "record with that host") {
			return &DNSConflictError{Hostname: hostname, TunnelName: tunnelName, Raw: combined}
		}
		return fmt.Errorf("route dns %s → %s: %w\n%s", hostname, tunnelName, err, combined)
	}
	// Zone-drift detection. When the requested hostname's base
	// domain isn't in the zone authorized by cert.pem, the CF API
	// silently appends the authorized zone as a suffix (see
	// cloudflared issue #1295). cloudflared's success line reports
	// the EFFECTIVE hostname; if it differs from what we asked for,
	// the caller's tunnel is wired to the wrong hostname and nothing
	// they do after this will work.
	if effective := parseEffectiveHostname(combined); effective != "" && effective != hostname {
		return &ZoneMismatchError{
			Requested: hostname,
			Effective: effective,
			Raw:       combined,
		}
	}
	return nil
}

// ZoneMismatchError is returned when cloudflared silently re-routes a
// DNS call to a hostname under the cert's authorized zone, appending
// the zone as a suffix — the cloudflared #1295 bug. The caller (CLI)
// maps this to a clear "your cert is authorized for zone X but you
// asked for Y; re-auth?" prompt.
type ZoneMismatchError struct {
	Requested string
	Effective string
	Raw       string
}

func (e *ZoneMismatchError) Error() string {
	return fmt.Sprintf(
		"zone mismatch: asked cloudflared to route %q but it created %q instead. "+
			"Your cloudflared login is authorized for a different zone than %q's base domain. "+
			"Re-run `cloudflared tunnel login` (or pass --re-login) and pick the Cloudflare account that owns %q.",
		e.Requested, e.Effective, e.Requested, e.Requested,
	)
}

// parseEffectiveHostname picks the hostname cloudflared reported in
// its success line out of its combined output. Two line formats
// exist across cloudflared versions:
//
//   <HOST> is already configured to route to your tunnel ...
//   <HOST> is now configured to route to your tunnel ...
//
// Returns "" when no match is found (shouldn't happen on success).
var effectiveHostRe = regexp.MustCompile(`([A-Za-z0-9][A-Za-z0-9.\-]*)\s+is\s+(?:already|now)\s+configured\s+to\s+route`)

func parseEffectiveHostname(combined string) string {
	m := effectiveHostRe.FindStringSubmatch(combined)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSuffix(m[1], ".")
}

// RunSetup orchestrates the full named-tunnel setup: verify cloudflared,
// login (unless skipped), create tunnel, route DNS, persist config. It
// does not prompt — the caller (CLI) handles interactive input.
func RunSetup(ctx context.Context, opts SetupOpts) (*SetupResult, error) {
	if opts.Hostname == "" {
		return nil, errors.New("hostname is required")
	}
	if opts.TunnelName == "" {
		opts.TunnelName = DefaultTunnelName
	}
	// --api-token path: full setup via CF REST API, no cert.pem.
	// cloudflared is still needed at listener-time to run the data
	// plane (we don't re-implement the tunnel proxy), but it's used
	// in token mode (`tunnel run --token <T>`), not cert mode.
	if opts.APIToken != "" {
		if err := CheckCloudflared(); err != nil {
			return nil, err
		}
		return runSetupViaAPI(ctx, opts)
	}
	if err := CheckCloudflared(); err != nil {
		return nil, err
	}
	// ForceLogin wipes cert.pem and re-authenticates so the user can
	// pick a different CF account. Used by the --re-login flag for
	// cases where the existing cert is bound to the wrong zone.
	if opts.ForceLogin {
		if err := resetCloudflaredCert(); err != nil {
			return nil, fmt.Errorf("reset cloudflared cert: %w", err)
		}
	}
	if !opts.SkipLogin && !HasCloudflaredCert() {
		if err := Login(ctx); err != nil {
			return nil, err
		}
	}

	// 60s cap per cloudflared operation to avoid hanging forever on
	// network flakes.
	opCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	id, err := createOrFindTunnel(opCtx, opts.TunnelName)
	if err != nil {
		return nil, err
	}
	err = routeDNS(opCtx, opts.TunnelName, opts.Hostname, opts.OverwriteDNS)
	// Auto-recover from zone-mismatch: the existing cert is authorized
	// for a different zone. Delete it, re-login, retry routeDNS once.
	// Skip when --re-login was already tried (infinite loop guard) or
	// when caller asked not to auto-retry.
	if zmErr := (*ZoneMismatchError)(nil); errors.As(err, &zmErr) && !opts.ForceLogin && !opts.NoAutoRelogin {
		if err := resetCloudflaredCert(); err != nil {
			return nil, fmt.Errorf("zone mismatch on %q — tried to reset cert.pem for re-auth but: %w", opts.Hostname, err)
		}
		if err := Login(ctx); err != nil {
			return nil, fmt.Errorf("zone mismatch on %q — cert.pem cleared; re-login failed: %w", opts.Hostname, err)
		}
		// Fresh cert → the tunnel itself may now belong to a different
		// account. Re-discover it.
		id, err = createOrFindTunnel(opCtx, opts.TunnelName)
		if err != nil {
			return nil, err
		}
		err = routeDNS(opCtx, opts.TunnelName, opts.Hostname, opts.OverwriteDNS)
	}
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		SchemaVersion: ConfigSchemaVersion,
		Mode:          ModeNamed,
		Hostname:      opts.Hostname,
		TunnelName:    opts.TunnelName,
		TunnelID:      id,
	}
	if err := SaveConfig(cfg); err != nil {
		return nil, err
	}
	return &SetupResult{
		Hostname:   cfg.Hostname,
		TunnelName: cfg.TunnelName,
		TunnelID:   cfg.TunnelID,
	}, nil
}

// runSetupViaAPI implements the --api-token path. All work goes
// through the CF REST API; cloudflared is only invoked at listener-
// time and only in --token mode. Returns the same SetupResult shape
// so the CLI handler doesn't branch.
func runSetupViaAPI(ctx context.Context, opts SetupOpts) (*SetupResult, error) {
	api := NewCFAPIClient(opts.APIToken)

	// 1. Account — pin if caller specified, otherwise pick the only
	// one the token sees. Multiple accounts + no pin = fail; caller
	// (CLI) handles the prompt in a separate pass.
	accountID := opts.APIAccountID
	if accountID == "" {
		accounts, err := api.ListAccounts(ctx)
		if err != nil {
			return nil, fmt.Errorf("list accounts (check token has Account:Read): %w", err)
		}
		switch len(accounts) {
		case 0:
			return nil, errors.New("token isn't authorized on any Cloudflare account")
		case 1:
			accountID = accounts[0].ID
		default:
			ids := make([]string, 0, len(accounts))
			for _, a := range accounts {
				ids = append(ids, fmt.Sprintf("  %s  %s", a.ID, a.Name))
			}
			return nil, fmt.Errorf("token is authorized on %d accounts — pick one with --api-account-id:\n%s", len(accounts), strings.Join(ids, "\n"))
		}
	}

	// 2. Zone — walk up labels until a matching authorized zone shows
	// up. Errors surface the "add this domain to CF first" remediation.
	zone, err := api.FindZoneForHostname(ctx, opts.Hostname)
	if err != nil {
		return nil, err
	}

	// 3. Tunnel — create or reuse by name.
	tun, err := api.CreateOrFindTunnel(ctx, accountID, opts.TunnelName)
	if err != nil {
		return nil, fmt.Errorf("create tunnel (check token has Account:Cloudflare Tunnel:Edit): %w", err)
	}

	// 4. Token for `cloudflared tunnel run --token <X>`. Fetching
	// here so the listener never needs the API token at runtime —
	// config stores only the tunnel-scoped credential.
	token, err := api.TunnelToken(ctx, accountID, tun.ID)
	if err != nil {
		return nil, fmt.Errorf("fetch tunnel token: %w", err)
	}

	// 5. DNS — create CNAME → <tunnel>.cfargotunnel.com. Honors
	// OverwriteDNS; returns DNSConflictError otherwise.
	if err := api.CreateDNSRecord(ctx, zone.ID, opts.Hostname, tun.ID, opts.OverwriteDNS); err != nil {
		return nil, err
	}

	cfg := &Config{
		SchemaVersion: ConfigSchemaVersion,
		Mode:          ModeNamed,
		Hostname:      opts.Hostname,
		TunnelName:    tun.Name,
		TunnelID:      tun.ID,
		TunnelToken:   token,
	}
	if err := SaveConfig(cfg); err != nil {
		return nil, err
	}
	return &SetupResult{
		Hostname:   cfg.Hostname,
		TunnelName: cfg.TunnelName,
		TunnelID:   cfg.TunnelID,
	}, nil
}
