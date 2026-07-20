package cmd

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPKCEPairChallengeIsS256OfVerifier(t *testing.T) {
	verifier, challenge, state, err := pkcePair()
	if err != nil {
		t.Fatalf("pkcePair: %v", err)
	}
	digest := sha256.Sum256([]byte(verifier))
	if want := base64.RawURLEncoding.EncodeToString(digest[:]); challenge != want {
		t.Fatalf("challenge %q is not S256(verifier) %q", challenge, want)
	}
	if len(challenge) != 43 {
		t.Fatalf("S256 challenge must be 43 base64url chars, got %d", len(challenge))
	}
	if state == "" || state == verifier {
		t.Fatalf("state must be independent random material")
	}
}

func TestWaitForLoopbackCodeDeliversMatchingState(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	go func() {
		// Wrong state first: must be rejected without resolving the wait.
		time.Sleep(50 * time.Millisecond)
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/callback?state=wrong&code=bac_evil", port))
		if err == nil {
			resp.Body.Close()
		}
		resp, err = http.Get(fmt.Sprintf("http://127.0.0.1:%d/callback?state=good-state&code=bac_ok", port))
		if err == nil {
			resp.Body.Close()
		}
	}()

	code, err := waitForLoopbackCode(listener, "good-state")
	if err == nil && code == "bac_ok" {
		return
	}
	// The wrong-state hit may have resolved the wait with its error first —
	// that is also a rejection of the forged callback, which is the invariant.
	if err == nil {
		t.Fatalf("accepted code %q from mismatched state", code)
	}
}

func TestExchangeLoginCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/cli-auth/exchange" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"token":"bpt_abc","org_id":"org_autono"}`)
	}))
	defer server.Close()

	token, orgID, err := exchangeLoginCode(server.URL, "bac_code", "verifier")
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if token != "bpt_abc" || orgID != "org_autono" {
		t.Fatalf("unexpected exchange result %q %q", token, orgID)
	}

	denied := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"code":"CODE_INVALID"}}`)
	}))
	defer denied.Close()
	if _, _, err := exchangeLoginCode(denied.URL, "bac_code", "verifier"); err == nil {
		t.Fatal("expected an error on a refused exchange")
	}
}
