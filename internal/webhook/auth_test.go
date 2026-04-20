package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Guards the four n8n-style auth paths end-to-end. Each test uses a
// plain httptest request (no tunnel/server) and feeds directly into
// VerifyAuth so these stay fast and deterministic.

func TestVerifyAuth_NoneAndNil(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "/x", nil)
	for _, cfg := range []*TriggerAuthConfig{nil, {Type: ""}, {Type: "none"}} {
		if res := VerifyAuth(cfg, req); !res.OK {
			t.Errorf("nil/empty/none cfg should pass, got %+v", res)
		}
	}
}

func TestVerifyAuth_BasicHappyPath(t *testing.T) {
	cfg := &TriggerAuthConfig{Type: "basic", Username: "u", Password: "p"}
	req, _ := http.NewRequest(http.MethodPost, "/x", nil)
	req.SetBasicAuth("u", "p")
	if res := VerifyAuth(cfg, req); !res.OK {
		t.Fatalf("basic should pass with matching creds: %+v", res)
	}
}

func TestVerifyAuth_BasicRejects(t *testing.T) {
	cfg := &TriggerAuthConfig{Type: "basic", Username: "u", Password: "p"}
	cases := []struct {
		name      string
		apply     func(*http.Request)
		wantCode  string
		wantStatus int
	}{
		{"missing", func(r *http.Request) {}, "AUTH_MISSING", http.StatusUnauthorized},
		{"wrong_user", func(r *http.Request) { r.SetBasicAuth("x", "p") }, "AUTH_INVALID", http.StatusUnauthorized},
		{"wrong_pass", func(r *http.Request) { r.SetBasicAuth("u", "x") }, "AUTH_INVALID", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodPost, "/x", nil)
			tc.apply(req)
			res := VerifyAuth(cfg, req)
			if res.OK {
				t.Fatalf("should have failed")
			}
			if res.Code != tc.wantCode || res.Status != tc.wantStatus {
				t.Errorf("got %s/%d, want %s/%d", res.Code, res.Status, tc.wantCode, tc.wantStatus)
			}
		})
	}
}

func TestVerifyAuth_HeaderHappyAndMismatch(t *testing.T) {
	cfg := &TriggerAuthConfig{Type: "header", HeaderName: "X-Token", HeaderValue: "s3cret"}
	req, _ := http.NewRequest(http.MethodPost, "/x", nil)
	req.Header.Set("X-Token", "s3cret")
	if res := VerifyAuth(cfg, req); !res.OK {
		t.Fatalf("should pass with matching header: %+v", res)
	}
	req2, _ := http.NewRequest(http.MethodPost, "/x", nil)
	req2.Header.Set("X-Token", "wrong")
	if res := VerifyAuth(cfg, req2); res.OK {
		t.Fatal("should reject on mismatch")
	}
	req3, _ := http.NewRequest(http.MethodPost, "/x", nil)
	// no header at all
	res := VerifyAuth(cfg, req3)
	if res.Code != "AUTH_MISSING" {
		t.Errorf("expected AUTH_MISSING on header absent, got %s", res.Code)
	}
}

func TestVerifyAuth_EnvRefResolution(t *testing.T) {
	t.Setenv("BUTTONS_TEST_SECRET", "env-val")
	cfg := &TriggerAuthConfig{Type: "header", HeaderName: "X-Token", HeaderValue: "$ENV{BUTTONS_TEST_SECRET}"}
	req, _ := http.NewRequest(http.MethodPost, "/x", nil)
	req.Header.Set("X-Token", "env-val")
	if res := VerifyAuth(cfg, req); !res.OK {
		t.Fatalf("env-backed header should match its resolved value, got %+v", res)
	}
}

// TestVerifyAuth_JWT runs through a full HS256 signing flow against
// our verifier using only stdlib primitives so the test doesn't
// depend on a JWT library the production code deliberately avoids.
func TestVerifyAuth_JWT(t *testing.T) {
	secret := "test-secret"
	token := signHS256Token(t, secret, map[string]any{
		"iss": "test-issuer",
		"aud": "test-aud",
		"exp": time.Now().Add(1 * time.Hour).Unix(),
	})

	cfg := &TriggerAuthConfig{Type: "jwt", JWTSecret: secret, JWTIssuer: "test-issuer", JWTAudience: "test-aud"}
	req, _ := http.NewRequest(http.MethodPost, "/x", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if res := VerifyAuth(cfg, req); !res.OK {
		t.Fatalf("valid JWT should pass: %+v", res)
	}

	t.Run("wrong_issuer", func(t *testing.T) {
		cfg2 := &TriggerAuthConfig{Type: "jwt", JWTSecret: secret, JWTIssuer: "other"}
		if res := VerifyAuth(cfg2, req); res.OK {
			t.Fatal("should reject issuer mismatch")
		}
	})
	t.Run("bad_signature", func(t *testing.T) {
		cfg2 := &TriggerAuthConfig{Type: "jwt", JWTSecret: "different-secret"}
		if res := VerifyAuth(cfg2, req); res.OK {
			t.Fatal("should reject signature mismatch")
		}
	})
	t.Run("expired", func(t *testing.T) {
		expired := signHS256Token(t, secret, map[string]any{
			"exp": time.Now().Add(-2 * time.Hour).Unix(),
		})
		req2, _ := http.NewRequest(http.MethodPost, "/x", nil)
		req2.Header.Set("Authorization", "Bearer "+expired)
		cfg2 := &TriggerAuthConfig{Type: "jwt", JWTSecret: secret}
		res := VerifyAuth(cfg2, req2)
		if res.OK {
			t.Fatal("should reject expired")
		}
		if !strings.Contains(res.Detail, "expired") {
			t.Errorf("expected 'expired' in detail, got %q", res.Detail)
		}
	})
	t.Run("missing_bearer", func(t *testing.T) {
		req2, _ := http.NewRequest(http.MethodPost, "/x", nil)
		cfg2 := &TriggerAuthConfig{Type: "jwt", JWTSecret: secret}
		if res := VerifyAuth(cfg2, req2); res.Code != "AUTH_MISSING" {
			t.Errorf("expected AUTH_MISSING, got %s", res.Code)
		}
	})
}

// signHS256Token builds a minimal JWT using only stdlib so our tests
// don't rely on golang-jwt/jwt — that's a deliberate avoidance in
// production code we don't want to undo just for tests.
func signHS256Token(t *testing.T, secret string, claims map[string]any) string {
	t.Helper()
	header := map[string]any{"alg": "HS256", "typ": "JWT"}
	hb, _ := json.Marshal(header)
	cb, _ := json.Marshal(claims)
	h := base64.RawURLEncoding.EncodeToString(hb)
	c := base64.RawURLEncoding.EncodeToString(cb)
	signingInput := h + "." + c
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return signingInput + "." + sig
}

// ensure httptest import used — avoids unused-import break under
// future refactors that might trim this file.
var _ = httptest.NewServer
