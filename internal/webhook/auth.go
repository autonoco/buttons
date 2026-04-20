package webhook

// Trigger-webhook authentication. Mirrors n8n's four auth types
// (None | Basic | Header | JWT) so agents familiar with n8n webhook
// nodes get an identical mental model. Every string-vs-string
// comparison runs through crypto/subtle so timing side-channels
// don't leak the configured secret.
//
// Secrets referenced as $ENV{VAR_NAME} in drawer.json are resolved at
// match time against the listener's process environment, so agents
// can commit a drawer.json that encodes the auth shape but not the
// secret itself.

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

// AuthResult is the structured outcome of a verification attempt.
// Separate from a plain error so the dispatcher can map cleanly to
// HTTP status codes and agent-facing error codes.
type AuthResult struct {
	// OK is true when the request satisfies the configured auth.
	OK bool
	// Code is a stable uppercase token used in the HTTP response body
	// so clients can distinguish "missing creds" from "wrong creds".
	Code string
	// Status is the HTTP status code the dispatcher should return.
	Status int
	// Detail is a short human string. Never includes secret material.
	Detail string
}

// authResultOK is the sentinel for a cleanly-passing check. Using a
// helper so call sites don't have to remember to set Status=200.
func authResultOK() AuthResult {
	return AuthResult{OK: true, Status: http.StatusOK, Code: "OK"}
}

// VerifyAuth checks the request against a drawer trigger's auth
// configuration. A nil or Type=="none" auth always passes — that's
// the open-endpoint case.
type TriggerAuthConfig struct {
	Type         string
	Username     string
	Password     string
	HeaderName   string
	HeaderValue  string
	JWTSecret    string
	JWTAlgorithm string
	JWTIssuer    string
	JWTAudience  string
}

// VerifyAuth is the single entry point the dispatcher calls.
// Resolves $ENV{VAR} references in the auth's string fields before
// comparing.
func VerifyAuth(cfg *TriggerAuthConfig, r *http.Request) AuthResult {
	if cfg == nil || cfg.Type == "" || cfg.Type == "none" {
		return authResultOK()
	}
	switch cfg.Type {
	case "basic":
		return verifyBasic(cfg, r)
	case "header":
		return verifyHeader(cfg, r)
	case "jwt":
		return verifyJWT(cfg, r)
	}
	return AuthResult{
		Code:   "AUTH_UNKNOWN_TYPE",
		Status: http.StatusInternalServerError,
		Detail: fmt.Sprintf("unknown auth type %q", cfg.Type),
	}
}

// --- basic ---

func verifyBasic(cfg *TriggerAuthConfig, r *http.Request) AuthResult {
	user, pass, ok := r.BasicAuth()
	if !ok {
		return AuthResult{
			Code:   "AUTH_MISSING",
			Status: http.StatusUnauthorized,
			Detail: "HTTP Basic credentials required",
		}
	}
	wantUser := resolveEnvRef(cfg.Username)
	wantPass := resolveEnvRef(cfg.Password)
	if !ctEqual(user, wantUser) || !ctEqual(pass, wantPass) {
		return AuthResult{
			Code:   "AUTH_INVALID",
			Status: http.StatusUnauthorized,
			Detail: "HTTP Basic credentials rejected",
		}
	}
	return authResultOK()
}

// --- header ---

func verifyHeader(cfg *TriggerAuthConfig, r *http.Request) AuthResult {
	got := r.Header.Get(cfg.HeaderName)
	want := resolveEnvRef(cfg.HeaderValue)
	if got == "" {
		return AuthResult{
			Code:   "AUTH_MISSING",
			Status: http.StatusUnauthorized,
			Detail: fmt.Sprintf("header %q required", cfg.HeaderName),
		}
	}
	if !ctEqual(got, want) {
		return AuthResult{
			Code:   "AUTH_INVALID",
			Status: http.StatusUnauthorized,
			Detail: fmt.Sprintf("header %q value rejected", cfg.HeaderName),
		}
	}
	return authResultOK()
}

// --- jwt ---

func verifyJWT(cfg *TriggerAuthConfig, r *http.Request) AuthResult {
	token := bearerToken(r)
	if token == "" {
		return AuthResult{
			Code:   "AUTH_MISSING",
			Status: http.StatusUnauthorized,
			Detail: "Authorization: Bearer <jwt> required",
		}
	}
	alg := cfg.JWTAlgorithm
	if alg == "" {
		alg = "HS256"
	}
	secret := resolveEnvRef(cfg.JWTSecret)
	claims, err := parseAndVerifyJWT(token, alg, secret)
	if err != nil {
		return AuthResult{
			Code:   "AUTH_INVALID",
			Status: http.StatusUnauthorized,
			Detail: "JWT rejected: " + err.Error(),
		}
	}
	if cfg.JWTIssuer != "" {
		if iss, _ := claims["iss"].(string); iss != cfg.JWTIssuer {
			return AuthResult{
				Code:   "AUTH_INVALID",
				Status: http.StatusUnauthorized,
				Detail: "JWT issuer mismatch",
			}
		}
	}
	if cfg.JWTAudience != "" && !jwtAudienceMatches(claims["aud"], cfg.JWTAudience) {
		return AuthResult{
			Code:   "AUTH_INVALID",
			Status: http.StatusUnauthorized,
			Detail: "JWT audience mismatch",
		}
	}
	return authResultOK()
}

// parseAndVerifyJWT decodes a compact-serialised JWT, verifies the
// signature with the configured HS-family algorithm, and enforces the
// `exp` claim if present. Returns the decoded claims map on success.
//
// We roll our own rather than importing golang-jwt/jwt because the
// stdlib has every primitive we need and the feature surface is
// small (HS256/384/512, exp enforcement, optional iss/aud). Avoids a
// transitive-dep footprint.
func parseAndVerifyJWT(token, alg, secret string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed (expected 3 segments)")
	}
	// Header check: confirm alg matches configured algorithm; reject
	// "none" and any asymmetric alg since we only verify HMAC.
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("bad header b64: %w", err)
	}
	var header struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, fmt.Errorf("bad header json: %w", err)
	}
	if !strings.EqualFold(header.Alg, alg) {
		return nil, fmt.Errorf("algorithm mismatch (token: %s, expected: %s)", header.Alg, alg)
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("bad payload b64: %w", err)
	}
	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("bad signature b64: %w", err)
	}

	// HMAC verification. Signing input is `header.payload` (the
	// base64-encoded originals, not the decoded bytes).
	signingInput := parts[0] + "." + parts[1]
	expected, err := computeHMAC(alg, secret, signingInput)
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare(sigBytes, expected) != 1 {
		return nil, errors.New("signature invalid")
	}

	var claims map[string]any
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, fmt.Errorf("bad claims json: %w", err)
	}
	// exp enforcement if present. Accept int or float (JSON number
	// decodes as float64 in Go). Allow 30s of clock skew.
	if rawExp, ok := claims["exp"]; ok {
		var expUnix int64
		switch v := rawExp.(type) {
		case float64:
			expUnix = int64(v)
		case int64:
			expUnix = v
		case int:
			expUnix = int64(v)
		default:
			return nil, errors.New("exp claim must be a number")
		}
		if time.Now().Add(-30 * time.Second).Unix() > expUnix {
			return nil, errors.New("token expired")
		}
	}
	return claims, nil
}

// computeHMAC returns the raw signature bytes for signingInput under
// the given HS-family algorithm.
func computeHMAC(alg, secret, signingInput string) ([]byte, error) {
	var h hash.Hash
	switch strings.ToUpper(alg) {
	case "HS256":
		h = hmac.New(sha256.New, []byte(secret))
	case "HS384":
		h = hmac.New(sha512.New384, []byte(secret))
	case "HS512":
		h = hmac.New(sha512.New, []byte(secret))
	default:
		return nil, fmt.Errorf("unsupported algorithm %q", alg)
	}
	h.Write([]byte(signingInput))
	return h.Sum(nil), nil
}

func jwtAudienceMatches(raw any, want string) bool {
	switch v := raw.(type) {
	case string:
		return v == want
	case []any:
		for _, a := range v {
			if s, ok := a.(string); ok && s == want {
				return true
			}
		}
	}
	return false
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	const pfx = "Bearer "
	if !strings.HasPrefix(h, pfx) {
		return ""
	}
	return strings.TrimSpace(h[len(pfx):])
}

// --- shared helpers ---

// ctEqual compares two strings in constant time relative to their
// length. Treats mismatched lengths as non-equal up front, which is
// the common behavior expected from a timing-safe comparator.
func ctEqual(got, want string) bool {
	if len(got) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// envRef matches $ENV{VAR_NAME} with a conservative var-name charset.
// Anchored so partial matches in the middle of strings aren't
// accidentally expanded.
var envRef = regexp.MustCompile(`^\$ENV\{([A-Za-z_][A-Za-z0-9_]*)\}$`)

// resolveEnvRef returns the environment value if v is exactly
// "$ENV{NAME}", otherwise returns v unchanged. We intentionally only
// support full-string references (not inline interpolation) for auth
// fields — mixing a literal prefix with a secret is almost always a
// configuration mistake, and the narrow contract keeps the error
// surface small.
func resolveEnvRef(v string) string {
	m := envRef.FindStringSubmatch(v)
	if m == nil {
		return v
	}
	return os.Getenv(m[1])
}
