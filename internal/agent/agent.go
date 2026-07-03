// Package agent is the device-identity + registration client for an agent
// workspace. Identity is an Ed25519 keypair generated on-device: the private
// key never leaves this machine, and the device id is the hash of the public
// key, so the identity is provable by signature rather than asserted.
//
// The registry base URL is NEVER hardcoded here — every call takes a BaseURL
// the caller sources from $BUTTONS_REGISTRY_URL. The one-time enrollment token
// is a battery (never committed), and the returned URLs come from the server,
// so this client carries no knowledge of any specific host.
package agent

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/autonoco/buttons/internal/config"
)

// ConfigSchemaVersion pins the shape of agent.json. Bump on breaks.
const ConfigSchemaVersion = 1

// Config is the on-disk device identity, stored 0600 in the data dir. DeviceSeed
// is the Ed25519 seed (the private key) — the durable credential; Slug is filled
// in once the workspace has registered.
type Config struct {
	SchemaVersion int    `json:"schema_version"`
	DeviceSeed    string `json:"device_seed"`    // base64 Ed25519 seed (32 bytes)
	Slug          string `json:"slug,omitempty"` // set once registered
}

// Identity is the key material derived from a Config's seed.
type Identity struct {
	priv     ed25519.PrivateKey
	PubB64   string // base64 of the 32-byte raw public key
	DeviceID string // hex(sha256(raw public key))
}

// ConfigPath resolves agent.json under the standard data directory (project-local
// .buttons/ beats global ~/.buttons/), so co-located agents each get their own.
func ConfigPath() (string, error) {
	base, err := config.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "agent.json"), nil
}

// LoadConfig returns the persisted identity, or (nil, nil) if none exists yet.
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

// SaveConfig writes the identity at 0600 (it holds the private key).
func SaveConfig(c *Config) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	c.SchemaVersion = ConfigSchemaVersion
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal agent config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	// Write to a temp file then atomically rename, so an interrupted write never
	// leaves a truncated device credential in place.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// LoadOrCreate returns the existing identity, generating and persisting a fresh
// keypair on first use. The generated seed is the durable device credential.
func LoadOrCreate() (*Config, error) {
	c, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	if c != nil && c.DeviceSeed != "" {
		return c, nil
	}
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return nil, fmt.Errorf("generate device key: %w", err)
	}
	c = &Config{SchemaVersion: ConfigSchemaVersion, DeviceSeed: base64.StdEncoding.EncodeToString(seed)}
	if err := SaveConfig(c); err != nil {
		return nil, err
	}
	return c, nil
}

// Identity derives the key material from the stored seed. device_id and the
// public-key encoding match what the registry computes server-side.
func (c *Config) Identity() (*Identity, error) {
	if c.DeviceSeed == "" {
		return nil, fmt.Errorf("no device key: run `buttons agent enroll` first")
	}
	seed, err := base64.StdEncoding.DecodeString(c.DeviceSeed)
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("corrupt device key in agent.json")
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	sum := sha256.Sum256(pub)
	return &Identity{priv: priv, PubB64: base64.StdEncoding.EncodeToString(pub), DeviceID: hex.EncodeToString(sum[:])}, nil
}

func (id *Identity) sign(msg string) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(id.priv, []byte(msg)))
}

// --- broker client ---

// Client talks to the registration broker. BaseURL is the caller-supplied
// $BUTTONS_REGISTRY_URL; nothing here embeds a host.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c *Client) do(ctx context.Context, method, p, bearer string, body any) (*http.Response, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+p, rdr)
	if err != nil {
		return nil, err
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		// Deliberately does not echo the URL — keep transport errors host-quiet.
		return nil, fmt.Errorf("registry request failed: %w", err)
	}
	return resp, nil
}

// BrokerError is a decoded {"error":{code,message}} response — carries the code
// so callers can branch (e.g. "device not enrolled").
type BrokerError struct {
	What    string
	Status  int
	Code    string
	Message string
}

func (e *BrokerError) Error() string {
	switch {
	case e.Code != "":
		return fmt.Sprintf("%s: %d %s (%s)", e.What, e.Status, e.Message, e.Code)
	case e.Message != "": // non-JSON body kept as raw detail
		return fmt.Sprintf("%s: %d %s", e.What, e.Status, e.Message)
	default:
		return fmt.Sprintf("%s: %d", e.What, e.Status)
	}
}

// IsNotEnrolled reports whether err is the broker's UNKNOWN_DEVICE response.
func IsNotEnrolled(err error) bool {
	var be *BrokerError
	return errors.As(err, &be) && be.Code == "UNKNOWN_DEVICE"
}

// brokerError decodes the {"error":{code,message}} envelope into a *BrokerError.
func brokerError(what string, resp *http.Response) error {
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &env) != nil || env.Error.Message == "" {
		// Not our JSON envelope (e.g. a proxy / load-balancer error page): keep the raw
		// body so the error still carries useful detail instead of just a status code.
		return &BrokerError{What: what, Status: resp.StatusCode, Message: strings.TrimSpace(string(data))}
	}
	return &BrokerError{What: what, Status: resp.StatusCode, Code: env.Error.Code, Message: env.Error.Message}
}

// EnrollResult is the device→owner binding the broker returns.
type EnrollResult struct {
	DeviceID  string  `json:"device_id"`
	OwnerID   string  `json:"owner_id"`
	Namespace *string `json:"namespace"`
}

// Enroll trades a one-time enroll token for a device→owner binding. Idempotent
// server-side for the same key + owner.
func (c *Client) Enroll(ctx context.Context, enrollToken, hostKind string, id *Identity) (*EnrollResult, error) {
	resp, err := c.do(ctx, http.MethodPost, "/v1/devices/enroll", enrollToken, map[string]string{
		"pubkey":    id.PubB64,
		"host_kind": hostKind,
	})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusCreated {
		return nil, brokerError("enroll", resp)
	}
	defer func() { _ = resp.Body.Close() }()
	var out EnrollResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("enroll: decode: %w", err)
	}
	return &out, nil
}

func (c *Client) challenge(ctx context.Context) (string, error) {
	resp, err := c.do(ctx, http.MethodGet, "/v1/challenge", "", nil)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", brokerError("challenge", resp)
	}
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		Nonce string `json:"nonce"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("challenge: decode: %w", err)
	}
	if out.Nonce == "" {
		return "", fmt.Errorf("challenge: empty nonce")
	}
	return out.Nonce, nil
}

// RegisterParams are the caller-chosen fields for a registration.
type RegisterParams struct {
	Slug      string
	TunnelID  string
	AgentID   string // optional
	Principal string // optional
}

// URLs is the derived URL set the broker returns (never constructed client-side).
type URLs struct {
	Webhook string  `json:"webhook"`
	Tunnel  string  `json:"tunnel"`
	Wake    string  `json:"wake"`
	Deploy  *string `json:"deploy"`
}

// RegisterResult is the broker's response — the slug's URLs come from here.
type RegisterResult struct {
	OK      bool   `json:"ok"`
	Slug    string `json:"slug"`
	Status  string `json:"status"`
	OwnerID string `json:"owner_id"`
	URLs    URLs   `json:"urls"`
	DNS     string `json:"dns"`
}

// Register fetches a fresh nonce, signs it into the registration message, and
// registers the workspace. The signature proves possession of the device key.
func (c *Client) Register(ctx context.Context, id *Identity, p RegisterParams) (*RegisterResult, error) {
	nonce, err := c.challenge(ctx)
	if err != nil {
		return nil, err
	}
	msg := "buttons-agent-register\n" + p.Slug + "\n" + p.TunnelID + "\n" + nonce
	body := map[string]string{
		"device_id": id.DeviceID,
		"slug":      p.Slug,
		"tunnel_id": p.TunnelID,
		"nonce":     nonce,
		"signature": id.sign(msg),
	}
	if p.AgentID != "" {
		body["agent_id"] = p.AgentID
	}
	if p.Principal != "" {
		body["principal"] = p.Principal
	}
	resp, err := c.do(ctx, http.MethodPost, "/v1/agents/register", "", body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, brokerError("register", resp)
	}
	defer func() { _ = resp.Body.Close() }()
	var out RegisterResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("register: decode: %w", err)
	}
	return &out, nil
}

// Setup is the one idempotent call behind `buttons agent setup`: register, and if
// the device isn't bound yet, enroll with the one-time token and register again.
// An already-registered device just re-points. Returns the not-enrolled BrokerError
// (IsNotEnrolled) when the device is unbound and no enroll token was supplied.
func (c *Client) Setup(ctx context.Context, id *Identity, enrollToken, hostKind string, p RegisterParams) (*RegisterResult, error) {
	res, err := c.Register(ctx, id, p)
	if !IsNotEnrolled(err) {
		return res, err // registered, or a non-enrollment failure
	}
	if enrollToken == "" {
		return nil, err // unbound + no token — let the caller surface a helpful message
	}
	if _, eerr := c.Enroll(ctx, enrollToken, hostKind, id); eerr != nil {
		return nil, eerr
	}
	return c.Register(ctx, id, p)
}
