package webhook

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Tunnel is a running cloudflared subprocess that exposes a public URL
// routing to the local Server.
type Tunnel struct {
	URL    string   // public https URL agents put into webhook config
	Mode   Mode     // ModeQuick or ModeNamed
	cmd    *exec.Cmd
	done   chan struct{}
	stderr *strings.Builder // last ~4KB of output, used when we error
}

// CloudflaredMissingError is returned when the binary isn't on PATH.
// The CLI uses this to print a clean install hint instead of a stack.
type CloudflaredMissingError struct{}

func (CloudflaredMissingError) Error() string {
	return "cloudflared not found on PATH — install via: brew install cloudflared"
}

// CheckCloudflared returns (nil) if the binary is usable. Uses
// `cloudflared --version` because it's cheap and side-effect-free.
func CheckCloudflared() error {
	if _, err := exec.LookPath("cloudflared"); err != nil {
		return CloudflaredMissingError{}
	}
	return nil
}

// StartTunnel launches cloudflared in the appropriate mode based on
// persisted config and waits until a public URL is known or ctx fires.
func StartTunnel(ctx context.Context, local string) (*Tunnel, error) {
	if err := CheckCloudflared(); err != nil {
		return nil, err
	}
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	if cfg != nil && cfg.Mode == ModeNamed {
		return startNamed(ctx, local, cfg)
	}
	return startQuick(ctx, local)
}

// quickURLPattern matches the line cloudflared prints when the Quick
// Tunnel is up. Example:
//   2026-04-20T12:00:00Z INF |  https://random-words.trycloudflare.com  |
var quickURLPattern = regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)

func startQuick(ctx context.Context, local string) (*Tunnel, error) {
	cmd := exec.Command("cloudflared", "tunnel",
		"--url", local,
		"--no-autoupdate",
	) // #nosec G204 -- arguments are literals + local loopback URL
	return runTunnel(ctx, cmd, ModeQuick, func(line string) (string, bool) {
		if m := quickURLPattern.FindString(line); m != "" {
			return m, true
		}
		return "", false
	}, "")
}

func startNamed(ctx context.Context, local string, cfg *Config) (*Tunnel, error) {
	if cfg.TunnelName == "" {
		return nil, errors.New("named tunnel config missing tunnel_name — run `buttons webhook setup`")
	}
	if cfg.Hostname == "" {
		return nil, errors.New("named tunnel config missing hostname — run `buttons webhook setup`")
	}
	cmd := exec.Command("cloudflared", "tunnel",
		"run",
		"--url", local,
		"--no-autoupdate",
		cfg.TunnelName,
	) // #nosec G204 -- TunnelName validated by setup flow
	url := "https://" + cfg.Hostname
	// For named tunnels we know the URL up front; the "ready" signal
	// is cloudflared's "Registered tunnel connection" log line.
	readyPattern := regexp.MustCompile(`(?i)registered tunnel connection|connection.*registered|serving http`)
	return runTunnel(ctx, cmd, ModeNamed, func(line string) (string, bool) {
		if readyPattern.MatchString(line) {
			return url, true
		}
		return "", false
	}, url)
}

// runTunnel starts the cloudflared subprocess and scans its combined
// output for a URL-ready signal. detectURL returns (url, true) on the
// first matching line; fallbackURL supplies a known URL for named-tunnel
// mode where the detector signals readiness rather than emitting a URL.
func runTunnel(
	ctx context.Context,
	cmd *exec.Cmd,
	mode Mode,
	detectURL func(string) (string, bool),
	fallbackURL string,
) (*Tunnel, error) {
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start cloudflared: %w", err)
	}

	t := &Tunnel{
		cmd:    cmd,
		Mode:   mode,
		done:   make(chan struct{}),
		stderr: &strings.Builder{},
	}

	urlCh := make(chan string, 1)
	var once sync.Once
	emit := func(u string) {
		once.Do(func() { urlCh <- u })
	}

	scan := func(r io.Reader) {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 64*1024), 512*1024)
		for sc.Scan() {
			line := sc.Text()
			// Keep a tail of recent output for error reporting.
			if t.stderr.Len() > 4096 {
				t.stderr.Reset()
			}
			t.stderr.WriteString(line)
			t.stderr.WriteByte('\n')
			if u, ok := detectURL(line); ok {
				emit(u)
			}
		}
	}
	go scan(stderr)
	go scan(stdout)
	go func() {
		_ = cmd.Wait()
		close(t.done)
	}()

	// First wait for cloudflared to print (or imply) a public URL.
	urlTimeout := 30 * time.Second
	var publicURL string
	select {
	case u := <-urlCh:
		publicURL = u
	case <-time.After(urlTimeout):
		if fallbackURL != "" {
			publicURL = fallbackURL
		} else {
			_ = t.Stop()
			return nil, fmt.Errorf("cloudflared did not emit a public URL within %s; last output:\n%s", urlTimeout, t.stderr.String())
		}
	case <-t.done:
		return nil, fmt.Errorf("cloudflared exited before emitting URL; output:\n%s", t.stderr.String())
	case <-ctx.Done():
		_ = t.Stop()
		return nil, ctx.Err()
	}

	// Cloudflared prints the URL before edge DNS + routing is fully
	// warm. Poll /healthz until it answers 200 so the URL we return to
	// the caller is actually usable. Without this, the first request a
	// drawer step makes often fails with "no such host" or 502.
	if err := waitForReady(ctx, publicURL, 60*time.Second); err != nil {
		_ = t.Stop()
		return nil, fmt.Errorf("tunnel URL %s did not become reachable: %w; last output:\n%s", publicURL, err, t.stderr.String())
	}
	t.URL = publicURL
	return t, nil
}

// waitForReady polls the tunnel's /healthz endpoint until it returns 2xx
// or the timeout elapses. Using the local Server's /healthz means we're
// testing the full chain — edge DNS, CF → local, local handler — not
// just any part of it.
func waitForReady(ctx context.Context, publicURL string, total time.Duration) error {
	deadline := time.Now().Add(total)
	backoff := 500 * time.Millisecond
	client := &http.Client{Timeout: 5 * time.Second}
	var lastErr error
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, publicURL+"/healthz", nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
		} else {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 4*time.Second {
			backoff *= 2
		}
	}
	if lastErr == nil {
		lastErr = errors.New("timeout")
	}
	return lastErr
}

// Stop terminates cloudflared. Safe to call multiple times.
func (t *Tunnel) Stop() error {
	if t == nil || t.cmd == nil || t.cmd.Process == nil {
		return nil
	}
	_ = t.cmd.Process.Signal(signalTerm)
	select {
	case <-t.done:
	case <-time.After(3 * time.Second):
		_ = t.cmd.Process.Kill()
	}
	return nil
}
