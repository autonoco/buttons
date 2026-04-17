package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/autonoco/buttons/internal/button"
)

// TestExecute_BatteriesInjectedAsEnv verifies the caller-provided
// batteries map lands on the child process as BUTTONS_BAT_<KEY>. This
// is the core contract of the batteries feature — a shell button has
// to be able to read a battery with no ceremony.
func TestExecute_BatteriesInjectedAsEnv(t *testing.T) {
	dir := t.TempDir()
	codePath := filepath.Join(dir, "main.sh")
	script := "#!/bin/sh\nprintf %s \"$BUTTONS_BAT_APIFY_TOKEN\"\n"
	if err := os.WriteFile(codePath, []byte(script), 0o700); err != nil { // #nosec G306 -- script must be executable
		t.Fatal(err)
	}

	btn := &button.Button{
		Name:           "echo-token",
		Runtime:        "shell",
		TimeoutSeconds: 5,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := Execute(ctx, btn, nil, map[string]string{"APIFY_TOKEN": "secret123"}, codePath)

	if result.Status != "ok" {
		t.Fatalf("status=%q stderr=%q", result.Status, result.Stderr)
	}
	if result.Stdout != "secret123" {
		t.Errorf("stdout = %q, want secret123", result.Stdout)
	}
}

// streamingServer returns an httptest server that streams `total`
// bytes of 'a' characters in 64 KB chunks with explicit flushes.
func streamingServer(total int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		chunk := strings.Repeat("a", 64<<10)
		written := 0
		for written < total {
			remaining := total - written
			if remaining < len(chunk) {
				chunk = chunk[:remaining]
			}
			n, err := w.Write([]byte(chunk))
			if err != nil {
				return
			}
			written += n
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
}

// TestExecuteHTTP_ResponseBodyLimit_Default verifies that a button with
// no declared MaxResponseBytes falls back to button.DefaultMaxResponseBytes
// (10 MB) and truncates oversize responses at exactly the cap.
func TestExecuteHTTP_ResponseBodyLimit_Default(t *testing.T) {
	payloadSize := int(button.DefaultMaxResponseBytes) + (1 << 20) // 10 MB + 1 MB overflow
	srv := streamingServer(payloadSize)
	defer srv.Close()

	btn := &button.Button{
		Name:                 "default-limit",
		Runtime:              "http",
		URL:                  srv.URL,
		Method:               "GET",
		TimeoutSeconds:       10,
		AllowPrivateNetworks: true, // httptest server binds 127.0.0.1
		// MaxResponseBytes intentionally left zero → defaults to 10 MB.
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result := Execute(ctx, btn, map[string]string{}, nil, "")

	if result.Status != "ok" {
		t.Fatalf("unexpected status %q; stderr=%q", result.Status, result.Stderr)
	}
	if got, want := int64(len(result.Stdout)), button.DefaultMaxResponseBytes; got != want {
		t.Errorf("body length = %d, want exactly %d (default cap)", got, want)
	}
}

// TestExecuteHTTP_ResponseBodyLimit_Custom verifies that a button with
// a custom MaxResponseBytes uses that value instead of the default, and
// that it works both for the over-cap truncation case and the small-body
// passthrough case.
func TestExecuteHTTP_ResponseBodyLimit_Custom(t *testing.T) {
	const customLimit int64 = 1 << 20 // 1 MB

	// Over-cap: 2 MB response vs 1 MB limit → truncate to 1 MB.
	t.Run("over_cap_truncated", func(t *testing.T) {
		srv := streamingServer(int(customLimit) + (512 << 10))
		defer srv.Close()

		btn := &button.Button{
			Name:                 "tight-limit",
			Runtime:              "http",
			URL:                  srv.URL,
			Method:               "GET",
			TimeoutSeconds:       10,
			MaxResponseBytes:     customLimit,
			AllowPrivateNetworks: true, // httptest server binds 127.0.0.1
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		result := Execute(ctx, btn, map[string]string{}, nil, "")
		if result.Status != "ok" {
			t.Fatalf("unexpected status %q; stderr=%q", result.Status, result.Stderr)
		}
		if got := int64(len(result.Stdout)); got != customLimit {
			t.Errorf("body length = %d, want exactly %d (custom cap)", got, customLimit)
		}
	})

	// Under-cap: 200 KB response vs 1 MB limit → passes through.
	t.Run("under_cap_passes_through", func(t *testing.T) {
		srv := streamingServer(200 << 10)
		defer srv.Close()

		btn := &button.Button{
			Name:                 "loose-limit",
			Runtime:              "http",
			URL:                  srv.URL,
			Method:               "GET",
			TimeoutSeconds:       10,
			MaxResponseBytes:     customLimit,
			AllowPrivateNetworks: true, // httptest server binds 127.0.0.1
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		result := Execute(ctx, btn, map[string]string{}, nil, "")
		if result.Status != "ok" {
			t.Fatalf("unexpected status %q; stderr=%q", result.Status, result.Stderr)
		}
		if got, want := len(result.Stdout), 200<<10; got != want {
			t.Errorf("body length = %d, want %d (full passthrough)", got, want)
		}
	})
}

// TestExecuteHTTP_SSRFBlocksLocalhostByDefault verifies that a URL
// button created without --allow-private-networks (the safe default)
// cannot reach an httptest server on 127.0.0.1. The executor must
// produce an error status with a stderr message that surfaces the
// SSRF rejection so users can diagnose + fix it.
func TestExecuteHTTP_SSRFBlocksLocalhostByDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok": true}`))
	}))
	defer srv.Close()

	btn := &button.Button{
		Name:           "blocked-local",
		Runtime:        "http",
		URL:            srv.URL,
		Method:         "GET",
		TimeoutSeconds: 5,
		// AllowPrivateNetworks intentionally NOT set → default false.
	}

	// Also explicitly clear the env var so a locally-exported
	// BUTTONS_ALLOW_PRIVATE_NETWORKS doesn't mask the test.
	t.Setenv("BUTTONS_ALLOW_PRIVATE_NETWORKS", "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := Execute(ctx, btn, map[string]string{}, nil, "")

	if result.Status != "error" {
		t.Fatalf("expected status 'error' for SSRF-blocked request, got %q", result.Status)
	}
	if result.Stderr == "" {
		t.Error("expected non-empty stderr describing the SSRF block")
	}
	if !strings.Contains(result.Stderr, "SSRF") && !strings.Contains(result.Stderr, "private") {
		t.Errorf("expected stderr to mention SSRF / private network, got %q", result.Stderr)
	}
}

// TestExecuteHTTP_SSRFPerButtonOverride verifies that setting
// AllowPrivateNetworks: true on the button unblocks localhost access.
func TestExecuteHTTP_SSRFPerButtonOverride(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok": true}`))
	}))
	defer srv.Close()

	btn := &button.Button{
		Name:                 "local-override",
		Runtime:              "http",
		URL:                  srv.URL,
		Method:               "GET",
		TimeoutSeconds:       5,
		AllowPrivateNetworks: true,
	}

	t.Setenv("BUTTONS_ALLOW_PRIVATE_NETWORKS", "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := Execute(ctx, btn, map[string]string{}, nil, "")

	if result.Status != "ok" {
		t.Fatalf("expected status 'ok' with per-button override, got %q; stderr=%q", result.Status, result.Stderr)
	}
	if result.Stdout != `{"ok": true}` {
		t.Errorf("expected body %q, got %q", `{"ok": true}`, result.Stdout)
	}
}

// TestExecuteHTTP_SSRFEnvVarOverride verifies that setting
// BUTTONS_ALLOW_PRIVATE_NETWORKS=1 unblocks localhost even when the
// button does NOT have the per-button flag.
func TestExecuteHTTP_SSRFEnvVarOverride(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok": true}`))
	}))
	defer srv.Close()

	btn := &button.Button{
		Name:           "env-override",
		Runtime:        "http",
		URL:            srv.URL,
		Method:         "GET",
		TimeoutSeconds: 5,
		// AllowPrivateNetworks NOT set — rely on env var.
	}

	t.Setenv("BUTTONS_ALLOW_PRIVATE_NETWORKS", "1")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := Execute(ctx, btn, map[string]string{}, nil, "")

	if result.Status != "ok" {
		t.Fatalf("expected status 'ok' with env var override, got %q; stderr=%q", result.Status, result.Stderr)
	}
}

// TestExecuteHTTP_SmallResponseUnaffected verifies that a normal-sized
// response (well under the cap) is returned in full.
func TestExecuteHTTP_SmallResponseUnaffected(t *testing.T) {
	const body = `{"ok": true, "value": 42}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	btn := &button.Button{
		Name:                 "small-response",
		Runtime:              "http",
		URL:                  srv.URL,
		Method:               "GET",
		TimeoutSeconds:       5,
		AllowPrivateNetworks: true, // httptest server binds 127.0.0.1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := Execute(ctx, btn, map[string]string{}, nil, "")

	if result.Status != "ok" {
		t.Fatalf("unexpected status %q; stderr=%q", result.Status, result.Stderr)
	}
	if result.Stdout != body {
		t.Errorf("Stdout = %q, want %q", result.Stdout, body)
	}
}
