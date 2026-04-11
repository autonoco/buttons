package engine

import (
	"context"
	"errors"
	"net"
	"testing"
)

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		// Loopback.
		{"127.0.0.1", true},
		{"127.255.255.254", true},
		// RFC 1918 private.
		{"10.0.0.1", true},
		{"10.255.255.255", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"192.168.0.1", true},
		{"192.168.255.255", true},
		// AWS/GCP metadata.
		{"169.254.169.254", true},
		// CGNAT.
		{"100.64.0.1", true},
		// IPv6 loopback + ULA + link-local.
		{"::1", true},
		{"fc00::1", true},
		{"fe80::1", true},
		// Public IPs (should NOT be blocked).
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"142.250.80.46", false},
		{"172.15.255.255", false}, // Just outside 172.16/12.
		{"172.32.0.1", false},     // Just outside 172.16/12 on the other side.
		{"192.167.255.255", false},
		{"192.169.0.0", false},
		{"2001:4860:4860::8888", false}, // Google Public DNS IPv6.
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("failed to parse %q", tt.ip)
			}
			if got := isPrivateIP(ip, privateNetworks); got != tt.want {
				t.Errorf("isPrivateIP(%s) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

func TestSafeDialContext_BlocksLiteralPrivateIP(t *testing.T) {
	dial := newSafeDialContext(privateNetworks)
	ctx := context.Background()

	tests := []string{
		"127.0.0.1:80",
		"10.0.0.1:443",
		"192.168.1.1:8080",
		"169.254.169.254:80",
		"[::1]:80",
		"[fc00::1]:80",
	}

	for _, addr := range tests {
		t.Run(addr, func(t *testing.T) {
			_, err := dial(ctx, "tcp", addr)
			if err == nil {
				t.Errorf("expected SSRFError for %s, got nil", addr)
				return
			}
			var ssrfErr *SSRFError
			if !errors.As(err, &ssrfErr) {
				t.Errorf("expected SSRFError for %s, got %T: %v", addr, err, err)
			}
		})
	}
}

func TestSafeDialContext_EmptyBlocklistAllowsAll(t *testing.T) {
	// An empty blocklist effectively disables SSRF protection. Used
	// when the button has --allow-private-networks set or the user
	// exported BUTTONS_ALLOW_PRIVATE_NETWORKS=1.
	dial := newSafeDialContext(nil)

	// We don't actually need the connection to succeed — we just
	// need to verify the block check is skipped. Use a ctx with an
	// immediate deadline so the dial fails fast with a network error
	// instead of an SSRFError.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := dial(ctx, "tcp", "127.0.0.1:1")
	if err == nil {
		return // Some test platforms allow this, fine.
	}
	var ssrfErr *SSRFError
	if errors.As(err, &ssrfErr) {
		t.Errorf("expected dial to proceed past SSRF check with nil blocklist; got %v", err)
	}
}

func TestSafeDialContext_BlocksPublicHostnameResolvingToPrivate(t *testing.T) {
	// If a hostname resolves to a private IP (think DNS hijack or
	// rebinding), the dialer rejects based on the resolved IP, not
	// the hostname. We simulate this using the standard "localhost"
	// hostname which is configured to resolve to 127.0.0.1 on any
	// sane system.
	dial := newSafeDialContext(privateNetworks)
	ctx := context.Background()

	_, err := dial(ctx, "tcp", "localhost:1")
	if err == nil {
		t.Fatal("expected SSRFError for localhost, got nil")
	}
	var ssrfErr *SSRFError
	if !errors.As(err, &ssrfErr) {
		t.Errorf("expected SSRFError for localhost, got %T: %v", err, err)
	}
}

func TestSafeDialContext_MalformedAddress(t *testing.T) {
	dial := newSafeDialContext(privateNetworks)
	ctx := context.Background()

	_, err := dial(ctx, "tcp", "not-a-valid-addr")
	if err == nil {
		t.Error("expected error for malformed address")
	}
	// Should NOT be an SSRFError — it's a structural parse error.
	var ssrfErr *SSRFError
	if errors.As(err, &ssrfErr) {
		t.Error("malformed address should not produce SSRFError")
	}
}

func TestPrivateNetworksGloballyAllowed(t *testing.T) {
	t.Run("unset is blocked", func(t *testing.T) {
		t.Setenv("BUTTONS_ALLOW_PRIVATE_NETWORKS", "")
		if privateNetworksGloballyAllowed() {
			t.Error("expected blocked when env unset")
		}
	})

	t.Run("zero is blocked", func(t *testing.T) {
		t.Setenv("BUTTONS_ALLOW_PRIVATE_NETWORKS", "0")
		if privateNetworksGloballyAllowed() {
			t.Error("expected blocked when env=0")
		}
	})

	t.Run("one is allowed", func(t *testing.T) {
		t.Setenv("BUTTONS_ALLOW_PRIVATE_NETWORKS", "1")
		if !privateNetworksGloballyAllowed() {
			t.Error("expected allowed when env=1")
		}
	})

	t.Run("other values are blocked", func(t *testing.T) {
		// Strict check: only the literal "1" unlocks it. "true", "yes",
		// etc. stay blocked to avoid surprises.
		for _, v := range []string{"true", "yes", "YES", "on", "2", " 1 "} {
			t.Setenv("BUTTONS_ALLOW_PRIVATE_NETWORKS", v)
			if privateNetworksGloballyAllowed() {
				t.Errorf("expected blocked for env=%q, got allowed", v)
			}
		}
	})
}
