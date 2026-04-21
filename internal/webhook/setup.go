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
	if err != nil {
		combined := string(out)
		if strings.Contains(combined, "already exists") || strings.Contains(combined, "record with that host") {
			return &DNSConflictError{Hostname: hostname, TunnelName: tunnelName, Raw: combined}
		}
		return fmt.Errorf("route dns %s → %s: %w\n%s", hostname, tunnelName, err, combined)
	}
	return nil
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
	if err := CheckCloudflared(); err != nil {
		return nil, err
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
	if err := routeDNS(opCtx, opts.TunnelName, opts.Hostname, opts.OverwriteDNS); err != nil {
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
