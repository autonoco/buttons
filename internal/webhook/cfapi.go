package webhook

// Cloudflare REST API client for the token-based setup path. Lets
// users bypass cloudflared's cert.pem entirely — they create a
// scoped API token in the CF dashboard and pass it to
// `buttons webhook setup --api-token`. Multi-zone capable (one token
// can be authorized for every zone they own), headless-friendly
// (no browser), and cleanly scoped (token can be revoked/rotated).
//
// Required token permissions:
//   Account — Cloudflare CFTunnel — Edit
//   Zone    — DNS               — Edit (on target zones)
//
// We implement exactly the four operations needed: resolve account,
// resolve zone, create tunnel (+ fetch its run-token), create DNS
// record. Everything else stays with cloudflared.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// cfAPIBase is the production CF REST API root. Tests can override
// via CFAPIClient.baseURL.
const cfAPIBase = "https://api.cloudflare.com/client/v4"

// CFAPIClient is a minimal CF API client scoped to the subset we use
// for webhook setup. One-shot usage; not thread-safe.
type CFAPIClient struct {
	token   string
	baseURL string
	http    *http.Client
}

// NewCFAPIClient constructs a client. The 30s timeout covers CF's
// typical response latency without letting a flaky network leave us
// hung during `buttons webhook setup`.
func NewCFAPIClient(token string) *CFAPIClient {
	return &CFAPIClient{
		token:   token,
		baseURL: cfAPIBase,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// cfEnvelope is the CF API's standard response shape. All endpoints
// return this; the concrete Result gets remarshalled into whatever
// type the caller expects.
type cfEnvelope struct {
	Success  bool              `json:"success"`
	Errors   []cfError         `json:"errors"`
	Messages []json.RawMessage `json:"messages"`
	Result   json.RawMessage   `json:"result"`
}

type cfError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// do performs an HTTP request against the CF API and unmarshals the
// Result section into `out`. Returns a CFAPIError with the first
// error's message + code on non-success.
func (c *CFAPIClient) do(ctx context.Context, method, path string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB cap — CF responses stay tiny
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	var env cfEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("parse envelope (status %d, body %q): %w", resp.StatusCode, string(data), err)
	}
	if !env.Success {
		if len(env.Errors) > 0 {
			e := env.Errors[0]
			return &CFAPIError{Code: e.Code, Message: e.Message, HTTPStatus: resp.StatusCode}
		}
		return &CFAPIError{HTTPStatus: resp.StatusCode, Message: fmt.Sprintf("unknown error (status %d)", resp.StatusCode)}
	}
	if out == nil || len(env.Result) == 0 {
		return nil
	}
	if err := json.Unmarshal(env.Result, out); err != nil {
		return fmt.Errorf("parse result: %w", err)
	}
	return nil
}

// CFAPIError wraps a Cloudflare API error response. Stable error code
// from CF's error catalog (e.g. 9109 = invalid permissions) so
// callers can branch on auth-vs-other failures.
type CFAPIError struct {
	Code       int
	Message    string
	HTTPStatus int
}

func (e *CFAPIError) Error() string {
	if e.Code > 0 {
		return fmt.Sprintf("cloudflare api: %s (code=%d, http=%d)", e.Message, e.Code, e.HTTPStatus)
	}
	return fmt.Sprintf("cloudflare api: %s (http=%d)", e.Message, e.HTTPStatus)
}

// Account holds the CF account fields we care about: id + name for
// the picker, and the authorized permissions for sanity-checking at
// setup time.
type Account struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ListAccounts returns every account the token is authorized on.
// A well-scoped token usually has exactly one — the user's personal
// account — but users with multiple accounts (org + personal) could
// see several and we surface the picker to them.
func (c *CFAPIClient) ListAccounts(ctx context.Context) ([]Account, error) {
	var out []Account
	if err := c.do(ctx, http.MethodGet, "/accounts?per_page=50", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Zone holds CF zone identity. Name is the apex (example.com), ID is
// the stable UUID we use in every DNS operation.
type Zone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// FindZoneForHostname resolves the CF zone a hostname belongs to.
// Starts with the full hostname and walks up its DNS labels — so
// "webhook.example.com" resolves against zone "example.com" (common
// case) or "webhook.example.com" itself (rare, when the subdomain
// is its own zone).
//
// Returns an error if no owned zone covers the hostname — that's
// actionable: the user needs to add the zone to CF first.
func (c *CFAPIClient) FindZoneForHostname(ctx context.Context, hostname string) (*Zone, error) {
	labels := strings.Split(hostname, ".")
	for i := 0; i < len(labels)-1; i++ { // stop before the TLD
		candidate := strings.Join(labels[i:], ".")
		var out []Zone
		if err := c.do(ctx, http.MethodGet, "/zones?name="+candidate+"&per_page=1", nil, &out); err != nil {
			return nil, err
		}
		if len(out) == 1 {
			return &out[0], nil
		}
	}
	return nil, fmt.Errorf("no Cloudflare zone covering %q is authorized on this API token — add the domain to CF (or grant Zone:Read to the token) and retry", hostname)
}

// CFTunnel is the CF API's cfd_tunnel result type, trimmed to fields
// we use downstream.
type CFTunnel struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	AccountID string    `json:"account_tag"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateOrFindTunnel creates a named tunnel on the given account or
// reuses an existing one if a tunnel with the same name is still
// alive. The tunnel-secret is a random 32-byte base64 string — CF
// accepts anything they haven't seen, so we generate fresh every
// create (harmless on reuse-branch since we discard it).
func (c *CFAPIClient) CreateOrFindTunnel(ctx context.Context, accountID, name string) (*CFTunnel, error) {
	// Reuse existing by name first — idempotent setup.
	var existing []CFTunnel
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/accounts/%s/cfd_tunnel?name=%s&is_deleted=false&per_page=1", accountID, name), nil, &existing); err != nil {
		return nil, err
	}
	if len(existing) == 1 {
		return &existing[0], nil
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("generate tunnel secret: %w", err)
	}
	body := map[string]any{
		"name":          name,
		"tunnel_secret": base64.StdEncoding.EncodeToString(secret),
		"config_src":    "local",
	}
	var out CFTunnel
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/accounts/%s/cfd_tunnel", accountID), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CFTunnelToken fetches the run-token for an existing tunnel. This is
// the base64-encoded credential you pass to `cloudflared tunnel run
// --token <TOKEN>` — no cert.pem needed. Scope matches the tunnel
// (can't run a different tunnel, can't touch DNS).
func (c *CFAPIClient) TunnelToken(ctx context.Context, accountID, tunnelID string) (string, error) {
	var out string
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/accounts/%s/cfd_tunnel/%s/token", accountID, tunnelID), nil, &out); err != nil {
		return "", err
	}
	return out, nil
}

// CreateDNSRecord creates a proxied CNAME at `name` pointing at
// `<tunnelID>.cfargotunnel.com` — the same target `cloudflared
// tunnel route dns` uses internally. `overwrite=true` deletes a
// conflicting existing record first; false returns DNSConflictError.
func (c *CFAPIClient) CreateDNSRecord(ctx context.Context, zoneID, name, tunnelID string, overwrite bool) error {
	target := tunnelID + ".cfargotunnel.com"
	// Pre-check: does a record already exist at this name?
	var existing []struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Content string `json:"content"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/zones/%s/dns_records?name=%s&per_page=1", zoneID, name), nil, &existing); err != nil {
		return err
	}
	if len(existing) == 1 {
		cur := existing[0]
		// Already pointing at this exact tunnel → idempotent success.
		if cur.Type == "CNAME" && cur.Content == target {
			return nil
		}
		if !overwrite {
			return &DNSConflictError{
				Hostname:   name,
				TunnelName: tunnelID,
				Raw:        fmt.Sprintf("record %s at %s → %s already exists (type=%s content=%q)", cur.ID, name, target, cur.Type, cur.Content),
			}
		}
		if err := c.do(ctx, http.MethodDelete, fmt.Sprintf("/zones/%s/dns_records/%s", zoneID, cur.ID), nil, nil); err != nil {
			return fmt.Errorf("delete existing record %s: %w", cur.ID, err)
		}
	}
	body := map[string]any{
		"type":    "CNAME",
		"name":    name,
		"content": target,
		"proxied": true,
		"ttl":     1, // 1 = automatic; required for proxied records
	}
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/zones/%s/dns_records", zoneID), body, nil)
}

// CFAPIAuthError is returned when the token is missing required
// permissions. Separated so the CLI can point at the exact token-
// scope fix.
type CFAPIAuthError struct {
	Missing string
	Raw     error
}

func (e *CFAPIAuthError) Error() string {
	return fmt.Sprintf("cloudflare api token is missing permission %q: %v", e.Missing, e.Raw)
}

func (e *CFAPIAuthError) Unwrap() error { return e.Raw }

// Stop linter complaint on an imported-but-conditionally-used module.
var _ = errors.New
