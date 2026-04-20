package webhook

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestNewCorrelationID_Uniqueness sanity-checks the random-id helper
// at scale so a future refactor can't accidentally introduce a
// collision-prone source (e.g. truncating the byte slice).
func TestNewCorrelationID_Uniqueness(t *testing.T) {
	const N = 10_000
	seen := make(map[string]struct{}, N)
	for i := 0; i < N; i++ {
		id, err := NewCorrelationID()
		if err != nil {
			t.Fatalf("NewCorrelationID: %v", err)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate id at iteration %d: %s", i, id)
		}
		seen[id] = struct{}{}
		if len(id) != 32 {
			t.Errorf("expected 32-char hex id, got %d chars", len(id))
		}
	}
}

// TestServer_RegisterBeforeReceive covers the primary flow we care
// about after dropping the pending map: Register returns a channel,
// the POST arrives, the channel delivers the event.
func TestServer_RegisterBeforeReceive(t *testing.T) {
	srv, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	id := "test-corr-1"
	ch := srv.Register(id)
	defer srv.Deregister(id)

	go func() {
		time.Sleep(20 * time.Millisecond) // simulate external round-trip
		postTo(t, srv.LocalURL()+"/webhook/"+id, `{"hello":"world"}`)
	}()

	select {
	case ev := <-ch:
		var body map[string]any
		_ = json.Unmarshal(ev.Body, &body)
		if body["hello"] != "world" {
			t.Errorf("unexpected body: %s", string(ev.Body))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for webhook delivery")
	}
}

// TestServer_WaitContextCancelled verifies Wait deregisters its
// waiter on ctx cancel — without that we'd slowly leak the waiters
// map over any long-running listener session that hits timeouts.
func TestServer_WaitContextCancelled(t *testing.T) {
	srv, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err = srv.Wait(ctx, "never-delivered")
	if err == nil {
		t.Fatal("expected ctx error, got nil")
	}
	// Map should be empty after deregister. Peek via Register: if the
	// id we just cancelled still owned a channel we'd overwrite it,
	// but with proper cleanup Register starts fresh.
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if _, still := srv.waiters["never-delivered"]; still {
		t.Error("cancelled waiter was not cleaned up from the map")
	}
}

// TestServer_HandleWebhook_RejectsSlashInID keeps the path parser
// honest. A request to /webhook/foo/bar should not match correlation
// id "foo/bar" — it's a clear client error.
func TestServer_HandleWebhook_RejectsSlashInID(t *testing.T) {
	srv, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	resp, err := http.Post(srv.LocalURL()+"/webhook/foo/bar", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
}

// TestServer_Healthz_TokenEmbedded proves the /healthz endpoint
// returns the readiness token so StartTunnel's waitForReady can
// distinguish a correctly-tunneled request from a stale-DNS 2xx.
func TestServer_Healthz_TokenEmbedded(t *testing.T) {
	srv, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	resp, err := http.Get(srv.LocalURL() + "/healthz")
	if err != nil {
		t.Fatalf("get healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
	var body struct {
		OK    bool   `json:"ok"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode healthz: %v", err)
	}
	if !body.OK {
		t.Error("expected ok=true in healthz body")
	}
	if body.Token == "" || body.Token != srv.ReadyToken() {
		t.Errorf("token mismatch: body=%q server=%q", body.Token, srv.ReadyToken())
	}
}

func postTo(t *testing.T, url, body string) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	_ = resp.Body.Close()
}
