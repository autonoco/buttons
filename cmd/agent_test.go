package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/autonoco/buttons/internal/agent"
	"github.com/autonoco/buttons/internal/webhook"
)

// fakeSetupBroker is a minimal registry for driving `agent setup` end to end.
// No enroll gate or signature check (crypto interop is covered in
// internal/agent) — it mints a tunnel + run-token only when the request
// carries no tunnel_id, mirroring the broker's fresh-provision rule.
func fakeSetupBroker(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/challenge", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"nonce": "test-nonce", "expires_at": 0})
	})
	mux.HandleFunc("/v1/agents/register", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		base := "https://" + body["slug"] + ".example.test"
		tunnelID := body["tunnel_id"]
		token := "" // run-token only when the broker just created the tunnel
		if tunnelID == "" {
			tunnelID, token = "tun-"+body["slug"], "token-"+body["slug"]
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "slug": body["slug"], "status": "created", "owner_id": "org_test",
			"urls": map[string]any{"webhook": base + "/hooks", "tunnel": base, "wake": base + "/wake", "deploy": nil},
			"dns":  "written", "tunnel_id": tunnelID, "tunnel_token": token,
		})
	})
	return httptest.NewServer(mux)
}

func runAgentSetup(t *testing.T, slug string) {
	t.Helper()
	agentSetupTunnel = ""
	agentSetupCmd.SetContext(context.Background())
	if err := agentSetupCmd.RunE(agentSetupCmd, []string{slug}); err != nil {
		t.Fatalf("agent setup: %v", err)
	}
}

func TestAgentSetupWritesWebhookConfig(t *testing.T) {
	t.Setenv("BUTTONS_HOME", t.TempDir())
	srv := fakeSetupBroker(t)
	defer srv.Close()
	t.Setenv("BUTTONS_REGISTRY_URL", srv.URL)

	runAgentSetup(t, "cindy")

	wc, err := webhook.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if wc == nil {
		t.Fatal("expected webhook config after fresh register")
	}
	if wc.Mode != webhook.ModeNamed || wc.Hostname != "cindy.example.test" ||
		wc.TunnelID != "tun-cindy" || wc.TunnelToken != "token-cindy" {
		t.Fatalf("unexpected webhook config: %+v", wc)
	}
}

func TestAgentSetupRerunKeepsWebhookConfig(t *testing.T) {
	t.Setenv("BUTTONS_HOME", t.TempDir())
	srv := fakeSetupBroker(t)
	defer srv.Close()
	t.Setenv("BUTTONS_REGISTRY_URL", srv.URL)

	runAgentSetup(t, "cindy")
	path, err := webhook.ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read webhook.json: %v", err)
	}

	// Re-run reuses the stored tunnel, so the broker returns no run-token and
	// the config written by the first run must survive byte for byte.
	runAgentSetup(t, "cindy")
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read webhook.json: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("re-run rewrote webhook.json:\nbefore: %s\nafter:  %s", before, after)
	}
}

func TestPersistWebhookConfigPreservesForeign(t *testing.T) {
	t.Setenv("BUTTONS_HOME", t.TempDir())
	orig := &webhook.Config{Mode: webhook.ModeNamed, Hostname: "keep.example.com", TunnelName: "keep", TunnelID: "tun-keep"}
	if err := webhook.SaveConfig(orig); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	res := &agent.RegisterResult{TunnelID: "tun-cindy", TunnelToken: "token-cindy"}
	res.URLs.Tunnel = "https://cindy.example.test"
	wrote, err := persistWebhookConfig(res)
	if err != nil {
		t.Fatalf("persistWebhookConfig: %v", err)
	}
	if wrote {
		t.Fatal("foreign webhook config must not be overwritten")
	}
	wc, err := webhook.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if wc.Hostname != "keep.example.com" || wc.TunnelName != "keep" || wc.TunnelID != "tun-keep" || wc.TunnelToken != "" {
		t.Fatalf("foreign webhook config changed: %+v", wc)
	}
}

func TestPersistWebhookConfigRefreshesOwnTunnel(t *testing.T) {
	t.Setenv("BUTTONS_HOME", t.TempDir())
	orig := &webhook.Config{Mode: webhook.ModeNamed, Hostname: "old.example.test", TunnelID: "tun-cindy", TunnelToken: "stale"}
	if err := webhook.SaveConfig(orig); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	res := &agent.RegisterResult{TunnelID: "tun-cindy", TunnelToken: "token-cindy"}
	res.URLs.Tunnel = "https://cindy.example.test"
	wrote, err := persistWebhookConfig(res)
	if err != nil {
		t.Fatalf("persistWebhookConfig: %v", err)
	}
	if !wrote {
		t.Fatal("expected same-tunnel config to be refreshed")
	}
	wc, err := webhook.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if wc.Hostname != "cindy.example.test" || wc.TunnelToken != "token-cindy" {
		t.Fatalf("unexpected webhook config: %+v", wc)
	}
}
