package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type memoryStore struct {
	credential *Credentials
	deleted    bool
	saveCalls  int
	saveErrors []error
}

func (s *memoryStore) Load(string) (Credentials, error) {
	if s.credential == nil {
		return Credentials{}, ErrCredentialsNotFound
	}
	return *s.credential, nil
}

func (s *memoryStore) Save(_ string, credential Credentials) error {
	call := s.saveCalls
	s.saveCalls++
	if call < len(s.saveErrors) && s.saveErrors[call] != nil {
		return s.saveErrors[call]
	}
	s.credential = &credential
	return nil
}

func (s *memoryStore) Delete(string) error {
	s.deleted = true
	s.credential = nil
	return nil
}

func TestAuthorizationServerMetadataURLFollowsRFC8414IssuerPaths(t *testing.T) {
	tests := map[string]string{
		"https://identity.example":          "https://identity.example/.well-known/oauth-authorization-server",
		"https://identity.example/tenant/a": "https://identity.example/.well-known/oauth-authorization-server/tenant/a",
	}
	for issuer, want := range tests {
		got, err := authorizationServerMetadataURL(issuer)
		if err != nil || got != want {
			t.Fatalf("authorizationServerMetadataURL(%q) = %q, %v; want %q", issuer, got, err, want)
		}
	}
}

func TestLoginUsesDiscoveryLoopbackPKCEAndStoresOneEnvelope(t *testing.T) {
	var server *httptest.Server
	var mu sync.Mutex
	tokenRequests := 0
	capabilityRequests := 0
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/cli-auth/config":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                  server.URL,
				"client_id":               "buttons-cli",
				"scopes":                  []string{"openid", "offline_access", "user:org:read"},
				"capability_exchange_url": server.URL + "/v1/cli-auth/exchange",
				"capability_revoke_url":   server.URL + "/v1/cli-auth/capability",
			})
		case "/.well-known/oauth-authorization-server":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"issuer":                 server.URL,
				"authorization_endpoint": server.URL + "/oauth/authorize",
				"token_endpoint":         server.URL + "/oauth/token",
				"revocation_endpoint":    server.URL + "/oauth/revoke",
			})
		case "/oauth/token":
			mu.Lock()
			tokenRequests++
			mu.Unlock()
			if r.FormValue("code_verifier") == "" || r.FormValue("code") != "provider-code" {
				http.Error(w, "invalid PKCE exchange", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "oauth-access",
				"refresh_token": "oauth-refresh",
				"token_type":    "Bearer",
				"expires_in":    3600,
			})
		case "/v1/cli-auth/exchange":
			mu.Lock()
			capabilityRequests++
			mu.Unlock()
			if r.Header.Get("Authorization") != "Bearer oauth-access" {
				http.Error(w, "missing bearer", http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token":      "bpt_login",
				"org_id":     "org_master",
				"expires_at": time.Now().Add(24 * time.Hour).Unix(),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	store := &memoryStore{}
	client := NewClient(store)
	client.OpenBrowser = func(target string) bool {
		authorize, err := url.Parse(target)
		if err != nil {
			t.Errorf("parse authorization URL: %v", err)
			return false
		}
		if authorize.Query().Get("code_challenge_method") != "S256" {
			t.Errorf("expected S256 PKCE URL, got %s", target)
		}
		callback := authorize.Query().Get("redirect_uri") + "?code=provider-code&state=" + url.QueryEscape(authorize.Query().Get("state"))
		go func() {
			response, err := http.Get(callback)
			if err == nil {
				_ = response.Body.Close()
			}
		}()
		return true
	}

	credential, err := client.Login(context.Background(), LoginOptions{
		RegistryURL: server.URL,
		Label:       "test laptop",
	})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if credential.CapabilityToken != "bpt_login" || credential.OrganizationID != "org_master" {
		t.Fatalf("unexpected credential: %#v", credential)
	}
	if store.credential == nil || store.credential.RefreshToken != "oauth-refresh" {
		t.Fatalf("credential envelope was not saved: %#v", store.credential)
	}
	if tokenRequests != 1 || capabilityRequests != 1 {
		t.Fatalf("expected one provider and capability exchange, got %d/%d", tokenRequests, capabilityRequests)
	}
}

func TestLoginRetainsProviderCredentialWhenCapabilityExchangeFails(t *testing.T) {
	server, openBrowser, _ := loginFailureFixture(t, http.StatusServiceUnavailable)
	defer server.Close()
	store := &memoryStore{}
	client := NewClient(store)
	client.OpenBrowser = openBrowser

	if _, err := client.Login(context.Background(), LoginOptions{RegistryURL: server.URL}); err == nil {
		t.Fatal("expected capability exchange failure")
	} else if !strings.Contains(err.Error(), "capability exchange failed") {
		t.Fatalf("unexpected login failure: %v", err)
	}
	if store.credential == nil || store.credential.RefreshToken != "oauth-refresh" || store.credential.CapabilityToken != "" {
		t.Fatalf("provider credential was not retained without a capability: %#v", store.credential)
	}
}

func TestLoginRevokesIssuedOAuthCredentialWhenInitialKeychainSaveFails(t *testing.T) {
	server, openBrowser, events := loginFailureFixture(t, http.StatusCreated)
	defer server.Close()
	store := &memoryStore{saveErrors: []error{errors.New("keychain unavailable")}}
	client := NewClient(store)
	client.OpenBrowser = openBrowser

	if _, err := client.Login(context.Background(), LoginOptions{RegistryURL: server.URL}); err == nil {
		t.Fatal("expected keychain failure")
	} else if !strings.Contains(err.Error(), "store keychain credential") {
		t.Fatalf("unexpected login failure: %v", err)
	}
	if strings.Join(*events, ",") != "oauth-revoke" {
		t.Fatalf("issued OAuth credential was not revoked: %v", *events)
	}
}

func TestLoginRevokesIssuedCapabilityWhenFinalKeychainSaveFails(t *testing.T) {
	server, openBrowser, events := loginFailureFixture(t, http.StatusCreated)
	defer server.Close()
	store := &memoryStore{saveErrors: []error{nil, errors.New("keychain unavailable")}}
	client := NewClient(store)
	client.OpenBrowser = openBrowser

	if _, err := client.Login(context.Background(), LoginOptions{RegistryURL: server.URL}); err == nil {
		t.Fatal("expected final keychain failure")
	} else if !strings.Contains(err.Error(), "store keychain credential") {
		t.Fatalf("unexpected login failure: %v", err)
	}
	if strings.Join(*events, ",") != "capability-revoke" {
		t.Fatalf("issued capability was not revoked: %v", *events)
	}
	if store.credential == nil || store.credential.RefreshToken != "oauth-refresh" || store.credential.CapabilityToken != "" {
		t.Fatalf("recoverable provider credential was not retained: %#v", store.credential)
	}
}

func loginFailureFixture(t *testing.T, capabilityStatus int) (*httptest.Server, func(string) bool, *[]string) {
	t.Helper()
	events := &[]string{}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/cli-auth/config":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer": server.URL, "client_id": "buttons-cli",
				"scopes":                  []string{"openid", "offline_access", "user:org:read"},
				"capability_exchange_url": server.URL + "/v1/cli-auth/exchange",
				"capability_revoke_url":   server.URL + "/v1/cli-auth/capability",
			})
		case "/.well-known/oauth-authorization-server":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"issuer": server.URL, "authorization_endpoint": server.URL + "/oauth/authorize",
				"token_endpoint": server.URL + "/oauth/token", "revocation_endpoint": server.URL + "/oauth/revoke",
			})
		case "/oauth/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "oauth-access", "refresh_token": "oauth-refresh", "token_type": "Bearer", "expires_in": 3600,
			})
		case "/v1/cli-auth/exchange":
			w.WriteHeader(capabilityStatus)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token": "bpt_login", "org_id": "org_master", "expires_at": time.Now().Add(24 * time.Hour).Unix(),
			})
		case "/v1/cli-auth/capability":
			*events = append(*events, "capability-revoke")
			w.WriteHeader(http.StatusOK)
		case "/oauth/revoke":
			*events = append(*events, "oauth-revoke")
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	openBrowser := func(target string) bool {
		authorize, err := url.Parse(target)
		if err != nil {
			t.Errorf("parse authorization URL: %v", err)
			return false
		}
		callback := authorize.Query().Get("redirect_uri") + "?code=provider-code&state=" + url.QueryEscape(authorize.Query().Get("state"))
		go func() {
			response, err := http.Get(callback)
			if err == nil {
				_ = response.Body.Close()
			}
		}()
		return true
	}
	return server, openBrowser, events
}

func TestCapabilityRefreshesExactlyOnceWhenExpired(t *testing.T) {
	providerCalls := 0
	capabilityCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			providerCalls++
			if r.FormValue("grant_type") != "refresh_token" || r.FormValue("refresh_token") != "refresh-old" {
				http.Error(w, "bad refresh", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "access-new", "refresh_token": "refresh-new", "token_type": "Bearer", "expires_in": 3600,
			})
		case "/v1/cli-auth/exchange":
			capabilityCalls++
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token": "bpt_new", "org_id": "org_master", "expires_at": time.Now().Add(24 * time.Hour).Unix(),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	store := &memoryStore{credential: &Credentials{
		RegistryURL: server.URL, ClientID: "buttons-cli", TokenEndpoint: server.URL + "/oauth/token",
		CapabilityExchangeURL: server.URL + "/v1/cli-auth/exchange", RefreshToken: "refresh-old",
		CapabilityToken: "bpt_old", CapabilityExpiresAt: time.Now().Add(-time.Minute).Unix(),
	}}
	client := NewClient(store)
	token, err := client.Capability(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("refresh capability: %v", err)
	}
	if token != "bpt_new" || store.credential.RefreshToken != "refresh-new" {
		t.Fatalf("unexpected refreshed credential: %q %#v", token, store.credential)
	}
	if providerCalls != 1 || capabilityCalls != 1 {
		t.Fatalf("expected one refresh and exchange, got %d/%d", providerCalls, capabilityCalls)
	}
}

func TestCapabilityPersistsRotatedRefreshTokenBeforeCapabilityExchange(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "access-new", "refresh_token": "refresh-new", "token_type": "Bearer", "expires_in": 3600,
			})
		case "/v1/cli-auth/exchange":
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "unavailable"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	store := &memoryStore{credential: &Credentials{
		RegistryURL: server.URL, ClientID: "buttons-cli", TokenEndpoint: server.URL + "/oauth/token",
		CapabilityExchangeURL: server.URL + "/v1/cli-auth/exchange", RefreshToken: "refresh-old",
		CapabilityToken: "bpt_old", CapabilityExpiresAt: time.Now().Add(-time.Minute).Unix(),
	}}
	if _, err := NewClient(store).Capability(context.Background(), server.URL); err == nil {
		t.Fatal("expected capability exchange failure")
	}
	if store.credential.RefreshToken != "refresh-new" || store.credential.AccessToken != "access-new" {
		t.Fatalf("rotated OAuth credential was not retained: %#v", store.credential)
	}
}

func TestCapabilityRequiresExplicitLoginToChangeOrganizations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token": "bpt_other", "org_id": "org_other", "expires_at": time.Now().Add(24 * time.Hour).Unix(),
		})
	}))
	defer server.Close()
	store := &memoryStore{credential: &Credentials{
		RegistryURL: server.URL, AccessToken: "still-live", OAuthExpiresAt: time.Now().Add(time.Hour).Unix(),
		CapabilityExchangeURL: server.URL, CapabilityRevokeURL: server.URL, CapabilityToken: "bpt_expired",
		CapabilityExpiresAt: time.Now().Add(-time.Minute).Unix(), OrganizationID: "org_master",
	}}
	if _, err := NewClient(store).Capability(context.Background(), server.URL); err == nil || !strings.Contains(err.Error(), "--switch-organization") {
		t.Fatalf("expected explicit switch error, got %v", err)
	}
	if store.credential.CapabilityToken != "bpt_expired" || store.credential.OrganizationID != "org_master" {
		t.Fatalf("organization changed without explicit login: %#v", store.credential)
	}
}

func TestLogoutRevokesBothCredentialsBeforeDeletingKeychainEnvelope(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path+":"+r.Header.Get("Authorization"))
		if r.URL.Path == "/oauth/revoke" && r.FormValue("token") != "refresh-token" {
			http.Error(w, "bad token", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store := &memoryStore{credential: &Credentials{
		RegistryURL: server.URL, ClientID: "buttons-cli", RefreshToken: "refresh-token",
		RevocationEndpoint: server.URL + "/oauth/revoke", CapabilityToken: "bpt_live",
		CapabilityExpiresAt: time.Now().Add(time.Hour).Unix(), CapabilityRevokeURL: server.URL + "/v1/cli-auth/capability",
	}}
	if err := NewClient(store).Logout(context.Background(), server.URL); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if !store.deleted {
		t.Fatal("credential envelope was not deleted")
	}
	joined := strings.Join(calls, ",")
	if joined != "/v1/cli-auth/capability:Bearer bpt_live,/oauth/revoke:" {
		t.Fatalf("unexpected revocation sequence: %s", joined)
	}
}

func TestLogoutKeepsEnvelopeWhenRevocationFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, fmt.Sprintf("failed %s", r.URL.Path), http.StatusServiceUnavailable)
	}))
	defer server.Close()
	store := &memoryStore{credential: &Credentials{
		RegistryURL: server.URL, ClientID: "buttons-cli", RefreshToken: "refresh-token",
		RevocationEndpoint: server.URL + "/oauth/revoke", CapabilityToken: "bpt_live",
		CapabilityExpiresAt: time.Now().Add(time.Hour).Unix(), CapabilityRevokeURL: server.URL + "/v1/cli-auth/capability",
	}}
	if err := NewClient(store).Logout(context.Background(), server.URL); err == nil {
		t.Fatal("expected revocation error")
	}
	if store.deleted {
		t.Fatal("credential envelope must remain recoverable after revocation failure")
	}
}

func TestLogoutFinishesWhenLiveAuthorityAlreadyDeniesTheCapability(t *testing.T) {
	providerRevoked := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/cli-auth/capability" {
			http.Error(w, "permission removed", http.StatusForbidden)
			return
		}
		providerRevoked = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	store := &memoryStore{credential: &Credentials{
		RegistryURL: server.URL, ClientID: "buttons-cli", RefreshToken: "refresh-token",
		RevocationEndpoint: server.URL + "/oauth/revoke", CapabilityToken: "bpt_denied",
		CapabilityExpiresAt: time.Now().Add(time.Hour).Unix(), CapabilityRevokeURL: server.URL + "/v1/cli-auth/capability",
	}}
	if err := NewClient(store).Logout(context.Background(), server.URL); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if !providerRevoked || !store.deleted {
		t.Fatal("logout must revoke the provider token and remove the denied capability envelope")
	}
}
