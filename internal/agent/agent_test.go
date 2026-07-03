package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeBroker stands in for the registry — entirely local (httptest), so no real
// host is involved. It reproduces the server's device_id formula and verifies the
// register signature exactly as the Worker does, so this also checks crypto interop.
func fakeBroker(t *testing.T) *httptest.Server {
	t.Helper()
	var enrolledPub []byte // captured at enroll, used to verify the register signature
	mux := http.NewServeMux()

	mux.HandleFunc("/v1/devices/enroll", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-enroll-token" {
			http.Error(w, `{"error":{"code":"UNAUTHORIZED","message":"bad token"}}`, http.StatusUnauthorized)
			return
		}
		var body struct{ Pubkey string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		pub, err := base64.StdEncoding.DecodeString(body.Pubkey)
		if err != nil || len(pub) != ed25519.PublicKeySize {
			http.Error(w, `{"error":{"code":"INVALID","message":"pubkey"}}`, http.StatusBadRequest)
			return
		}
		enrolledPub = pub
		sum := sha256.Sum256(pub)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"device_id": hex.EncodeToString(sum[:]), "owner_id": "org_test"})
	})

	mux.HandleFunc("/v1/challenge", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"nonce": "test-nonce", "expires_at": 0})
	})

	mux.HandleFunc("/v1/agents/register", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if enrolledPub == nil { // device not bound yet — the signal Setup auto-enrolls on
			http.Error(w, `{"error":{"code":"UNKNOWN_DEVICE","message":"device not enrolled"}}`, http.StatusUnauthorized)
			return
		}
		msg := "buttons-agent-register\n" + body["slug"] + "\n" + body["tunnel_id"] + "\n" + body["nonce"]
		sig, err := base64.StdEncoding.DecodeString(body["signature"])
		if err != nil || !ed25519.Verify(enrolledPub, []byte(msg), sig) {
			http.Error(w, `{"error":{"code":"BAD_SIGNATURE","message":"bad sig"}}`, http.StatusUnauthorized)
			return
		}
		base := "https://" + body["slug"] + ".example.test"
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "slug": body["slug"], "status": "created", "owner_id": "org_test",
			"urls": map[string]any{"webhook": base + "/hooks", "tunnel": base, "wake": base + "/wake", "deploy": nil},
			"dns":  "written",
		})
	})

	return httptest.NewServer(mux)
}

func TestEnrollThenRegister(t *testing.T) {
	t.Setenv("BUTTONS_HOME", t.TempDir()) // agent.json lands here, not the real data dir
	srv := fakeBroker(t)
	defer srv.Close()

	c, err := LoadOrCreate()
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	id, err := c.Identity()
	if err != nil {
		t.Fatalf("Identity: %v", err)
	}
	client := agentClientFor(srv)

	enr, err := client.Enroll(context.Background(), "test-enroll-token", "test/arch", id)
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if enr.DeviceID != id.DeviceID {
		t.Fatalf("device_id mismatch: server %s, client %s", enr.DeviceID, id.DeviceID)
	}

	res, err := client.Register(context.Background(), id, RegisterParams{Slug: "cindy", TunnelID: "tun_1", Principal: "bobak"})
	if err != nil {
		t.Fatalf("Register: %v", err) // fails if the signature didn't verify server-side
	}
	if res.Slug != "cindy" || res.Status != "created" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if res.URLs.Tunnel != "https://cindy.example.test" {
		t.Fatalf("urls come from the server; got %q", res.URLs.Tunnel)
	}
}

func TestEnrollRejectsBadToken(t *testing.T) {
	t.Setenv("BUTTONS_HOME", t.TempDir())
	srv := fakeBroker(t)
	defer srv.Close()
	c, _ := LoadOrCreate()
	id, _ := c.Identity()
	if _, err := agentClientFor(srv).Enroll(context.Background(), "wrong", "test/arch", id); err == nil {
		t.Fatal("expected enroll to reject a bad token")
	}
}

func agentClientFor(srv *httptest.Server) *Client {
	return &Client{BaseURL: srv.URL, HTTP: srv.Client()}
}

func TestSetupAutoEnrollsThenReRegisters(t *testing.T) {
	t.Setenv("BUTTONS_HOME", t.TempDir())
	srv := fakeBroker(t)
	defer srv.Close()
	c, _ := LoadOrCreate()
	id, _ := c.Identity()
	client := agentClientFor(srv)

	// First run: device unbound → Setup auto-enrolls with the token, then registers.
	res, err := client.Setup(context.Background(), id, "test-enroll-token", "test/arch", RegisterParams{Slug: "cindy", TunnelID: "t1"})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if res.Slug != "cindy" || res.Status != "created" {
		t.Fatalf("unexpected: %+v", res)
	}

	// Re-run: already enrolled → registers again with NO token needed.
	if _, err := client.Setup(context.Background(), id, "", "test/arch", RegisterParams{Slug: "cindy", TunnelID: "t2"}); err != nil {
		t.Fatalf("Setup re-run: %v", err)
	}
}

func TestSetupWithoutTokenWhenUnboundIsNotEnrolled(t *testing.T) {
	t.Setenv("BUTTONS_HOME", t.TempDir())
	srv := fakeBroker(t)
	defer srv.Close()
	c, _ := LoadOrCreate()
	id, _ := c.Identity()
	_, err := agentClientFor(srv).Setup(context.Background(), id, "", "test/arch", RegisterParams{Slug: "x", TunnelID: "t"})
	if !IsNotEnrolled(err) {
		t.Fatalf("expected IsNotEnrolled, got %v", err)
	}
}
