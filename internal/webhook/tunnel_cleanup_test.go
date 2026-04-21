package webhook

import (
	"context"
	"testing"
)

// TestCleanupCloudflaredConnectors_InvokesRunner verifies the cleanup
// helper forwards the tunnel name to the (test-swappable) runner. This
// is what startNamed relies on to drop stale edge connectors before a
// fresh `tunnel run`.
func TestCleanupCloudflaredConnectors_InvokesRunner(t *testing.T) {
	prev := cleanupCloudflaredRunner
	t.Cleanup(func() { cleanupCloudflaredRunner = prev })

	var got string
	var calls int
	cleanupCloudflaredRunner = func(_ context.Context, name string) {
		calls++
		got = name
	}

	cleanupCloudflaredConnectors(context.Background(), "buttons")
	if calls != 1 {
		t.Fatalf("runner call count = %d, want 1", calls)
	}
	if got != "buttons" {
		t.Fatalf("runner received name %q, want %q", got, "buttons")
	}
}

// TestCleanupCloudflaredConnectors_SkipsEmptyName guards the early
// return — an empty name would otherwise ask cloudflared to clean up
// every tunnel the user owns, which is absolutely not what we want.
func TestCleanupCloudflaredConnectors_SkipsEmptyName(t *testing.T) {
	prev := cleanupCloudflaredRunner
	t.Cleanup(func() { cleanupCloudflaredRunner = prev })

	var calls int
	cleanupCloudflaredRunner = func(_ context.Context, _ string) {
		calls++
	}

	cleanupCloudflaredConnectors(context.Background(), "")
	if calls != 0 {
		t.Fatalf("runner was invoked for empty name (calls=%d); should have been skipped", calls)
	}
}

// TestStartTunnel_QuickModeSkipsCleanup ensures ModeQuick (no persisted
// config, no cert.pem identity) never calls the edge cleanup. Quick
// tunnels are ephemeral trycloudflare.com subdomains with no persistent
// identity — running `cloudflared tunnel cleanup` against them is
// meaningless and would target the wrong thing.
//
// We drive the quick path by calling StartTunnel with no config file
// present and expect the runner to stay at zero calls even if the
// subprocess Start fails (which it will without a real cloudflared,
// but the gating logic runs first in startNamed — in startQuick it
// never runs at all).
func TestStartTunnel_QuickModeSkipsCleanup(t *testing.T) {
	prev := cleanupCloudflaredRunner
	t.Cleanup(func() { cleanupCloudflaredRunner = prev })

	var calls int
	cleanupCloudflaredRunner = func(_ context.Context, _ string) {
		calls++
	}

	// Bypass the named path by passing a quick-mode cfg equivalent
	// (nil cfg = quick in StartTunnel). We short-circuit before
	// actually spawning cloudflared by giving an already-canceled
	// context.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = StartTunnel(ctx, "http://127.0.0.1:1", "tkn")

	if calls != 0 {
		t.Fatalf("quick mode unexpectedly invoked edge cleanup (calls=%d); cleanup must only run for named tunnels with a persistent identity", calls)
	}
}
