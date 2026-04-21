// Package webhook exposes a local HTTP listener through a Cloudflare
// tunnel so drawer steps can register a public URL with a third-party
// service, wait for the service to POST back, and resume.
//
// Two modes:
//
//  - "quick": no config required. Each run spawns a fresh Cloudflare
//    Quick Tunnel. The public URL is ephemeral (*.trycloudflare.com)
//    and changes every invocation. Works for any service that accepts
//    a per-run webhook URL (Apify, etc.).
//
//  - "named": a stable hostname on the user's own Cloudflare account,
//    configured once via `buttons webhook setup`. Required when a
//    service wants a fixed URL registered once (GitHub webhooks).
//
// Config is stored at ~/.buttons/webhook.json so it's per-machine and
// survives across projects. Nothing secret is kept here — the actual
// Cloudflare credentials live under ~/.cloudflared/ where the
// `cloudflared` binary manages them.
package webhook

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/autonoco/buttons/internal/config"
)

// ConfigSchemaVersion pins the shape of webhook.json. Bump on breaks.
const ConfigSchemaVersion = 1

// Mode is the runtime mode for exposing the local listener.
type Mode string

const (
	// ModeQuick uses Cloudflared's anonymous trycloudflare.com tunnels.
	ModeQuick Mode = "quick"
	// ModeNamed uses a user-owned Cloudflare tunnel with a stable DNS
	// hostname. Set up via `buttons webhook setup`.
	ModeNamed Mode = "named"
)

// Config is the on-disk webhook connection.
type Config struct {
	SchemaVersion int    `json:"schema_version"`
	Mode          Mode   `json:"mode"`
	Hostname      string `json:"hostname,omitempty"`
	TunnelName    string `json:"tunnel_name,omitempty"`
	TunnelID      string `json:"tunnel_id,omitempty"`
	// TunnelToken is the opaque base64 credential `cloudflared
	// tunnel run --token <X>` expects. Populated when setup used the
	// --api-token path (CF REST API). When non-empty, the listener
	// runs tunnel-via-token instead of tunnel-via-name so cert.pem
	// isn't required at runtime.
	TunnelToken string `json:"tunnel_token,omitempty"`
}

// ConfigPath returns ~/.buttons/webhook.json using the standard data
// directory resolution (project-local .buttons/ beats global ~/.buttons/).
func ConfigPath() (string, error) {
	base, err := config.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "webhook.json"), nil
}

// LoadConfig returns the persisted webhook config. A missing file maps
// to (nil, nil) — callers fall back to quick-tunnel semantics.
func LoadConfig() (*Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path) // #nosec G304 -- fixed path under DataDir
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &c, nil
}

// SaveConfig writes the config atomically at 0600.
func SaveConfig(c *Config) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	c.SchemaVersion = ConfigSchemaVersion
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// DeleteConfig removes the webhook config file. Missing = no-op.
func DeleteConfig() error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// EffectiveMode reports which mode the CLI will use if a webhook step
// runs right now. No config = quick.
func EffectiveMode() (Mode, error) {
	c, err := LoadConfig()
	if err != nil {
		return "", err
	}
	if c == nil || c.Mode == "" {
		return ModeQuick, nil
	}
	return c.Mode, nil
}
