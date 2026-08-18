package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

var ErrCredentialsNotFound = errors.New("buttons credentials not found")

type Credentials struct {
	RegistryURL           string   `json:"registry_url"`
	Issuer                string   `json:"issuer"`
	ClientID              string   `json:"client_id"`
	Scopes                []string `json:"scopes"`
	TokenEndpoint         string   `json:"token_endpoint"`
	RevocationEndpoint    string   `json:"revocation_endpoint"`
	CapabilityExchangeURL string   `json:"capability_exchange_url"`
	CapabilityRevokeURL   string   `json:"capability_revoke_url"`
	AccessToken           string   `json:"access_token"`
	RefreshToken          string   `json:"refresh_token"`
	OAuthExpiresAt        int64    `json:"oauth_expires_at"`
	CapabilityToken       string   `json:"capability_token"`
	CapabilityExpiresAt   int64    `json:"capability_expires_at"`
	OrganizationID        string   `json:"organization_id"`
	Label                 string   `json:"label"`
}

type Store interface {
	Load(registryURL string) (Credentials, error)
	Save(registryURL string, credential Credentials) error
	Delete(registryURL string) error
}

type LoginOptions struct {
	RegistryURL        string
	Label              string
	NoBrowser          bool
	SwitchOrganization bool
}

type Client struct {
	Store       Store
	HTTP        *http.Client
	OpenBrowser func(string) bool
	Output      io.Writer
	Now         func() time.Time
}

type registryConfig struct {
	Issuer                string   `json:"issuer"`
	ClientID              string   `json:"client_id"`
	Scopes                []string `json:"scopes"`
	CapabilityExchangeURL string   `json:"capability_exchange_url"`
	CapabilityRevokeURL   string   `json:"capability_revoke_url"`
}

type providerMetadata struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	RevocationEndpoint    string `json:"revocation_endpoint"`
}

type capabilityResponse struct {
	Token     string `json:"token"`
	OrgID     string `json:"org_id"`
	ExpiresAt int64  `json:"expires_at"`
}

func NewClient(store Store) *Client {
	return &Client{
		Store:  store,
		HTTP:   http.DefaultClient,
		Output: os.Stderr,
		Now:    time.Now,
	}
}

func normalizeRegistryURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(raw, "/"))
	if err != nil || parsed.Host == "" {
		return "", fmt.Errorf("invalid registry URL %q", raw)
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && (parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "localhost")) {
		return "", fmt.Errorf("registry URL must use HTTPS")
	}
	return parsed.String(), nil
}

func authorizationServerMetadataURL(rawIssuer string) (string, error) {
	normalized, err := normalizeRegistryURL(rawIssuer)
	if err != nil {
		return "", err
	}
	issuer, err := url.Parse(normalized)
	if err != nil || issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" {
		return "", errors.New("OAuth issuer must not contain userinfo, a query, or a fragment")
	}
	return issuer.Scheme + "://" + issuer.Host + "/.well-known/oauth-authorization-server" + issuer.EscapedPath(), nil
}

func (c *Client) Login(ctx context.Context, options LoginOptions) (Credentials, error) {
	registryURL, err := normalizeRegistryURL(options.RegistryURL)
	if err != nil {
		return Credentials{}, err
	}
	existing, existingErr := c.Store.Load(registryURL)
	if existingErr == nil {
		if !options.SwitchOrganization {
			return Credentials{}, errors.New("already logged in; use --switch-organization to replace the current organization")
		}
		if err := c.revokeCredential(ctx, existing); err != nil {
			return Credentials{}, fmt.Errorf("revoke current login: %w", err)
		}
		if err := c.Store.Delete(registryURL); err != nil {
			return Credentials{}, fmt.Errorf("remove current keychain credential: %w", err)
		}
	} else if !errors.Is(existingErr, ErrCredentialsNotFound) {
		return Credentials{}, fmt.Errorf("read keychain credential: %w", existingErr)
	}

	registry, err := fetchJSON[registryConfig](ctx, c.HTTP, registryURL+"/v1/cli-auth/config")
	if err != nil {
		return Credentials{}, fmt.Errorf("discover registry authentication: %w", err)
	}
	metadataURL, err := authorizationServerMetadataURL(registry.Issuer)
	if err != nil {
		return Credentials{}, fmt.Errorf("invalid OAuth issuer: %w", err)
	}
	metadata, err := fetchJSON[providerMetadata](ctx, c.HTTP, metadataURL)
	if err != nil {
		return Credentials{}, fmt.Errorf("discover OAuth provider: %w", err)
	}
	if metadata.Issuer != registry.Issuer {
		return Credentials{}, errors.New("OAuth provider issuer does not match registry discovery")
	}
	for _, endpoint := range []string{
		registry.Issuer,
		registry.CapabilityExchangeURL,
		registry.CapabilityRevokeURL,
		metadata.AuthorizationEndpoint,
		metadata.TokenEndpoint,
		metadata.RevocationEndpoint,
	} {
		if _, err := normalizeRegistryURL(endpoint); err != nil {
			return Credentials{}, fmt.Errorf("unsafe OAuth endpoint: %w", err)
		}
	}
	if metadata.AuthorizationEndpoint == "" || metadata.TokenEndpoint == "" || metadata.RevocationEndpoint == "" {
		return Credentials{}, errors.New("OAuth provider metadata is incomplete")
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Credentials{}, fmt.Errorf("start loopback listener: %w", err)
	}
	defer listener.Close()
	redirectURL := "http://" + listener.Addr().String() + "/callback"
	verifier := oauth2.GenerateVerifier()
	state, err := randomState()
	if err != nil {
		return Credentials{}, fmt.Errorf("generate OAuth state: %w", err)
	}
	config := oauth2.Config{
		ClientID:    registry.ClientID,
		RedirectURL: redirectURL,
		Scopes:      registry.Scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:   metadata.AuthorizationEndpoint,
			TokenURL:  metadata.TokenEndpoint,
			AuthStyle: oauth2.AuthStyleInParams,
		},
	}
	authorizeURL := config.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.S256ChallengeOption(verifier))
	opened := false
	if !options.NoBrowser && c.OpenBrowser != nil {
		opened = c.OpenBrowser(authorizeURL)
	}
	if !opened {
		fmt.Fprintf(c.Output, "Open this URL in your browser to authorize:\n\n  %s\n\n", authorizeURL)
	}
	code, err := waitForLoopbackCode(ctx, listener, state)
	if err != nil {
		return Credentials{}, err
	}
	oauthContext := context.WithValue(ctx, oauth2.HTTPClient, c.HTTP)
	token, err := config.Exchange(oauthContext, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return Credentials{}, fmt.Errorf("exchange OAuth authorization code: %w", err)
	}
	if token.RefreshToken == "" {
		return Credentials{}, errors.New("OAuth provider did not issue an offline refresh token")
	}
	label := strings.TrimSpace(options.Label)
	credential := Credentials{
		RegistryURL: registryURL, Issuer: registry.Issuer, ClientID: registry.ClientID,
		Scopes: registry.Scopes, TokenEndpoint: metadata.TokenEndpoint, RevocationEndpoint: metadata.RevocationEndpoint,
		CapabilityExchangeURL: registry.CapabilityExchangeURL, CapabilityRevokeURL: registry.CapabilityRevokeURL,
		AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, OAuthExpiresAt: token.Expiry.Unix(),
		Label: label,
	}
	if err := c.Store.Save(registryURL, credential); err != nil {
		revokeErr := c.revokeOAuthRefreshToken(ctx, credential)
		if revokeErr != nil {
			return Credentials{}, fmt.Errorf("store keychain credential: %w; revoke issued OAuth credential: %v", err, revokeErr)
		}
		return Credentials{}, fmt.Errorf("store keychain credential: %w", err)
	}
	capability, err := c.exchangeCapability(ctx, registry.CapabilityExchangeURL, token.AccessToken, label)
	if err != nil {
		return Credentials{}, err
	}
	credential.CapabilityToken = capability.Token
	credential.CapabilityExpiresAt = capability.ExpiresAt
	credential.OrganizationID = capability.OrgID
	if err := c.Store.Save(registryURL, credential); err != nil {
		revokeErr := c.revokeCapability(ctx, registry.CapabilityRevokeURL, capability.Token)
		if revokeErr != nil {
			return Credentials{}, fmt.Errorf("store keychain credential: %w; revoke issued capability: %v", err, revokeErr)
		}
		return Credentials{}, fmt.Errorf("store keychain credential: %w", err)
	}
	return credential, nil
}

func randomState() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func waitForLoopbackCode(ctx context.Context, listener net.Listener, state string) (string, error) {
	type result struct {
		code string
		err  error
	}
	results := make(chan result, 1)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback" {
			http.NotFound(w, r)
			return
		}
		query := r.URL.Query()
		if query.Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}
		if providerError := query.Get("error"); providerError != "" {
			fmt.Fprintln(w, "Authorization denied. You can close this tab.")
			results <- result{err: fmt.Errorf("login denied in browser: %s", providerError)}
			return
		}
		if query.Get("code") == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			return
		}
		fmt.Fprintln(w, "Machine authorized. You can close this tab and return to the terminal.")
		results <- result{code: query.Get("code")}
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	select {
	case result := <-results:
		return result.code, result.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (c *Client) Capability(ctx context.Context, registryURL string) (string, error) {
	registryURL, err := normalizeRegistryURL(registryURL)
	if err != nil {
		return "", err
	}
	credential, err := c.Store.Load(registryURL)
	if err != nil {
		return "", err
	}
	now := c.Now().Unix()
	if credential.CapabilityToken != "" && credential.CapabilityExpiresAt > now {
		return credential.CapabilityToken, nil
	}
	accessToken := credential.AccessToken
	if accessToken == "" || credential.OAuthExpiresAt <= now {
		refreshed, err := c.refreshOAuth(ctx, credential)
		if err != nil {
			return "", err
		}
		credential.AccessToken = refreshed.AccessToken
		credential.OAuthExpiresAt = refreshed.Expiry.Unix()
		if refreshed.RefreshToken != "" {
			credential.RefreshToken = refreshed.RefreshToken
		}
		accessToken = refreshed.AccessToken
		if err := c.Store.Save(registryURL, credential); err != nil {
			return "", fmt.Errorf("store refreshed OAuth credential: %w", err)
		}
	}
	capability, err := c.exchangeCapability(ctx, credential.CapabilityExchangeURL, accessToken, credential.Label)
	if err != nil {
		return "", err
	}
	if credential.OrganizationID != "" && capability.OrgID != credential.OrganizationID {
		if err := c.revokeCapability(ctx, credential.CapabilityRevokeURL, capability.Token); err != nil {
			return "", fmt.Errorf("organization changed; revoke replacement capability: %w", err)
		}
		return "", errors.New("organization changed; run `buttons login --switch-organization`")
	}
	credential.CapabilityToken = capability.Token
	credential.CapabilityExpiresAt = capability.ExpiresAt
	credential.OrganizationID = capability.OrgID
	if err := c.Store.Save(registryURL, credential); err != nil {
		return "", fmt.Errorf("store refreshed keychain credential: %w", err)
	}
	return capability.Token, nil
}

func (c *Client) refreshOAuth(ctx context.Context, credential Credentials) (*oauth2.Token, error) {
	values := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {credential.RefreshToken},
		"client_id":     {credential.ClientID},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, credential.TokenEndpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := c.HTTP.Do(request)
	if err != nil {
		return nil, fmt.Errorf("refresh OAuth token: %w", err)
	}
	defer response.Body.Close()
	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode OAuth refresh response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || payload.AccessToken == "" {
		return nil, fmt.Errorf("OAuth refresh failed (HTTP %d)", response.StatusCode)
	}
	return &oauth2.Token{
		AccessToken: payload.AccessToken, RefreshToken: payload.RefreshToken, TokenType: payload.TokenType,
		Expiry: c.Now().Add(time.Duration(payload.ExpiresIn) * time.Second),
	}, nil
}

func (c *Client) exchangeCapability(ctx context.Context, endpoint, accessToken, label string) (capabilityResponse, error) {
	body, err := json.Marshal(map[string]string{"label": label})
	if err != nil {
		return capabilityResponse{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return capabilityResponse{}, err
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.HTTP.Do(request)
	if err != nil {
		return capabilityResponse{}, fmt.Errorf("exchange publish capability: %w", err)
	}
	defer response.Body.Close()
	var capability capabilityResponse
	if err := json.NewDecoder(response.Body).Decode(&capability); err != nil {
		return capabilityResponse{}, fmt.Errorf("decode capability response: %w", err)
	}
	if response.StatusCode != http.StatusCreated || capability.Token == "" || capability.ExpiresAt == 0 {
		return capabilityResponse{}, fmt.Errorf("capability exchange failed (HTTP %d)", response.StatusCode)
	}
	return capability, nil
}

func (c *Client) Logout(ctx context.Context, registryURL string) error {
	registryURL, err := normalizeRegistryURL(registryURL)
	if err != nil {
		return err
	}
	credential, err := c.Store.Load(registryURL)
	if err != nil {
		return err
	}
	if err := c.revokeCredential(ctx, credential); err != nil {
		return err
	}
	if err := c.Store.Delete(registryURL); err != nil {
		return fmt.Errorf("delete keychain credential: %w", err)
	}
	return nil
}

func (c *Client) revokeCredential(ctx context.Context, credential Credentials) error {
	if credential.CapabilityToken != "" && credential.CapabilityExpiresAt > c.Now().Unix() {
		if err := c.revokeCapability(ctx, credential.CapabilityRevokeURL, credential.CapabilityToken); err != nil {
			return err
		}
	}
	return c.revokeOAuthRefreshToken(ctx, credential)
}

func (c *Client) revokeOAuthRefreshToken(ctx context.Context, credential Credentials) error {
	if credential.RefreshToken == "" {
		return nil
	}
	values := url.Values{
		"token":           {credential.RefreshToken},
		"token_type_hint": {"refresh_token"},
		"client_id":       {credential.ClientID},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, credential.RevocationEndpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := c.HTTP.Do(request)
	if err != nil {
		return fmt.Errorf("revoke OAuth refresh token: %w", err)
	}
	_ = response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("revoke OAuth refresh token failed (HTTP %d)", response.StatusCode)
	}
	return nil
}

func (c *Client) revokeCapability(ctx context.Context, endpoint, token string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := c.HTTP.Do(request)
	if err != nil {
		return fmt.Errorf("revoke publish capability: %w", err)
	}
	_ = response.Body.Close()
	if (response.StatusCode < 200 || response.StatusCode >= 300) &&
		response.StatusCode != http.StatusUnauthorized &&
		response.StatusCode != http.StatusForbidden {
		return fmt.Errorf("revoke publish capability failed (HTTP %d)", response.StatusCode)
	}
	return nil
}

func fetchJSON[T any](ctx context.Context, client *http.Client, endpoint string) (T, error) {
	var result T
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return result, err
	}
	response, err := client.Do(request)
	if err != nil {
		return result, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return result, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return result, err
	}
	return result, nil
}
