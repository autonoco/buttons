package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/autonoco/buttons/internal/button"
)

const sigTermGrace = 5 * time.Second

// Execute runs a button with the given args and returns a Result.
// For code/file buttons, codePath is the path to the code file in the button folder.
// batteries is the caller-provided map of battery KEY → VALUE; each entry is
// injected into the child process as BUTTONS_BAT_<KEY> so shell / code buttons
// can read secrets without baking them into the script file. Pass nil to skip.
// sink, when non-nil, receives every line the child writes to stdout / stderr
// in real time as LogLine values — used by the TUI log viewer. The buffered
// Result.Stdout / Result.Stderr remain authoritative; the sink is best-effort
// (see stream.go for the back-pressure policy).
func Execute(ctx context.Context, btn *button.Button, args, batteries map[string]string, sink LineSink, codePath string) *Result {
	start := time.Now()
	result := &Result{
		Button:    btn.Name,
		Args:      args,
		StartedAt: start,
	}

	// HTTP buttons use a different execution path
	if btn.URL != "" {
		return executeHTTP(ctx, btn, args, result)
	}

	// Prompt buttons return the instruction from AGENT.md
	if btn.Runtime == "prompt" {
		return executePrompt(ctx, codePath, result)
	}

	// Resolve interpreter for the button's runtime
	interpreter, err := interpreterForRuntime(btn.Runtime)
	if err != nil {
		result.Status = "error"
		result.ExitCode = -1
		result.ErrorType = "RUNTIME_MISSING"
		result.Stderr = err.Error()
		result.DurationMs = time.Since(start).Milliseconds()
		return result
	}

	// Verify code file exists
	if _, err := os.Stat(codePath); err != nil {
		result.Status = "error"
		result.ExitCode = -1
		result.ErrorType = "SCRIPT_ERROR"
		result.Stderr = fmt.Sprintf("code file not found: %s", codePath)
		result.DurationMs = time.Since(start).Milliseconds()
		return result
	}

	// #nosec G204 -- `interpreter` is whitelisted by interpreterForRuntime() which
	// only returns /bin/sh, a resolved python3/python, or a resolved node — never
	// a user-supplied string. `codePath` is inside the button's own folder under
	// ButtonsDir and is not influenced by button args at press time. Args reach the
	// script exclusively via BUTTONS_ARG_* env vars (see cmd.Env below), so there
	// is no string interpolation into a shell command.
	cmd := exec.Command(interpreter, codePath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Build environment: inherit current + batteries + button env + args.
	// Order matters: batteries precede button env which precede args, so a
	// button-defined Env can override a battery and a per-press arg can
	// override either (matches the specificity users expect — most specific
	// wins). Keys exposed to the child process:
	//   BUTTONS_BAT_<KEY>  battery value
	//   BUTTONS_ARG_<NAME> arg value
	env := os.Environ()
	for k, v := range batteries {
		env = append(env, "BUTTONS_BAT_"+k+"="+v)
	}
	for k, v := range btn.Env {
		env = append(env, k+"="+v)
	}
	for k, v := range args {
		envName := "BUTTONS_ARG_" + strings.ToUpper(strings.ReplaceAll(k, "-", "_"))
		env = append(env, envName+"="+v)
	}
	// Export BUTTONS_PROGRESS_PATH so long-running buttons can stream
	// structured progress events (JSONL) into a file that `buttons
	// tail` follows in real time. We create the file empty so tailers
	// can start before the first write lands. No fd-3 magic — scripts
	// just append to $BUTTONS_PROGRESS_PATH.
	progressPath := defaultProgressPath(btn.Name, start)
	if progressPath != "" {
		if err := os.MkdirAll(filepathDir(progressPath), 0o700); err == nil {
			// #nosec G304 G306 -- progressPath is scoped inside the
			// button's pressed/ dir (same perms as the .json history
			// file alongside). 0600 keeps it user-private.
			if f, err := os.OpenFile(progressPath, os.O_CREATE|os.O_RDWR, 0o600); err == nil {
				_ = f.Close()
				env = append(env, "BUTTONS_PROGRESS_PATH="+progressPath)
				result.ProgressPath = progressPath
			}
		}
	}
	cmd.Env = env

	// Capture everything into buffers (authoritative for Result) and,
	// if a sink was provided, tee every completed line to it tagged
	// with the right severity. Partial trailing lines are emitted on
	// Flush after the child exits.
	var stdout, stderr bytes.Buffer
	stdoutTee := newLineTee(&stdout, sink, SeverityStdout)
	stderrTee := newLineTee(&stderr, sink, SeverityStderr)
	cmd.Stdout = stdoutTee
	cmd.Stderr = stderrTee

	if err := cmd.Start(); err != nil {
		result.Status = "error"
		result.ExitCode = -1
		result.ErrorType = "SCRIPT_ERROR"
		result.Stderr = err.Error()
		result.DurationMs = time.Since(start).Milliseconds()
		return result
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		result.DurationMs = time.Since(start).Milliseconds()
		// Emit any trailing partial lines before the consumer sees the
		// final Result — ensures the last message on a script that
		// didn't terminate its output with \n still appears in the
		// stream.
		stdoutTee.Flush()
		stderrTee.Flush()
		result.Stdout = stdout.String()
		result.Stderr = stderr.String()

		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				result.Status = "error"
				result.ExitCode = exitErr.ExitCode()
				result.ErrorType = "SCRIPT_ERROR"
			} else {
				result.Status = "error"
				result.ExitCode = -1
				result.ErrorType = "SCRIPT_ERROR"
				result.Stderr = err.Error()
			}
			return result
		}

		result.Status = "ok"
		result.ExitCode = 0
		return result

	case <-ctx.Done():
		killProcessGroup(cmd, done)
		result.DurationMs = time.Since(start).Milliseconds()
		stdoutTee.Flush()
		stderrTee.Flush()
		result.Stdout = stdout.String()
		result.Stderr = stderr.String()
		result.Status = "timeout"
		result.ExitCode = -1
		result.ErrorType = "TIMEOUT"
		return result
	}
}

// interpreterForRuntime maps a runtime name to the interpreter binary path.
func interpreterForRuntime(runtime string) (string, error) {
	switch runtime {
	case "shell", "sh", "bash", "":
		return "/bin/sh", nil
	case "python", "python3":
		if path, err := exec.LookPath("python3"); err == nil {
			return path, nil
		}
		if path, err := exec.LookPath("python"); err == nil {
			return path, nil
		}
		return "", fmt.Errorf("python not found on PATH")
	case "node", "javascript", "js":
		if path, err := exec.LookPath("node"); err == nil {
			return path, nil
		}
		return "", fmt.Errorf("node not found on PATH")
	default:
		return "", fmt.Errorf("unsupported runtime: %s", runtime)
	}
}

// executePrompt reads the AGENT.md instruction and returns it as output.
func executePrompt(ctx context.Context, promptPath string, result *Result) *Result {
	start := result.StartedAt

	// Respect context cancellation / timeout
	if ctx.Err() != nil {
		result.Status = "timeout"
		result.ExitCode = -1
		result.ErrorType = "TIMEOUT"
		result.Stderr = "timed out before reading prompt instruction"
		result.DurationMs = time.Since(start).Milliseconds()
		return result
	}

	// #nosec G304 -- promptPath is constructed by callers from config.ButtonDir()
	// (which rejects paths escaping ButtonsDir) + the literal "AGENT.md" suffix.
	data, err := os.ReadFile(promptPath)
	if err != nil {
		result.Status = "error"
		result.ExitCode = -1
		result.ErrorType = "PROMPT_ERROR"
		result.Stderr = fmt.Sprintf("failed to read AGENT.md: %v", err)
		result.DurationMs = time.Since(start).Milliseconds()
		return result
	}

	result.Status = "ok"
	result.ExitCode = 0
	result.Stdout = string(data)
	result.DurationMs = time.Since(start).Milliseconds()
	return result
}

// executeHTTP performs an HTTP request for URL-based buttons.
func executeHTTP(ctx context.Context, btn *button.Button, args map[string]string, result *Result) *Result {
	start := result.StartedAt

	// Substitute {{arg}} placeholders in the URL with context-aware
	// encoding (path segments get PathEscape, query values get
	// QueryEscape, fragment gets PathEscape). See SubstituteURL docs
	// in substitute.go for the escape matrix and threat model.
	rawURL := SubstituteURL(btn.URL, args)

	// SSRF guard: the button's {{arg}} values can carry data that
	// originated from a remote source (webhook POST body, for
	// instance). Enforce scheme + host constraints before dispatch so
	// an attacker who controls an arg can't pivot a button into an
	// arbitrary request against the operator's private network.
	safeURL, err := validateHTTPTarget(rawURL, btn.AllowPrivateNetworks)
	if err != nil {
		result.Status = "error"
		result.ExitCode = -1
		result.ErrorType = "VALIDATION_ERROR"
		result.Stderr = fmt.Sprintf("refusing request: %v", err)
		result.DurationMs = time.Since(start).Milliseconds()
		return result
	}

	method := btn.Method
	if method == "" {
		method = "GET"
	}

	var reqBody io.Reader
	if btn.Body != "" {
		// Content-Type-aware {{arg}} escaping for the body:
		// JSON values are JSON-escaped, form values are URL-encoded,
		// unknown content types fall through to raw substitution.
		body := SubstituteBody(btn.Body, args, btn.Headers)
		reqBody = strings.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, safeURL, reqBody)
	if err != nil {
		result.Status = "error"
		result.ExitCode = -1
		result.ErrorType = "SCRIPT_ERROR"
		result.Stderr = fmt.Sprintf("failed to create request: %v", err)
		result.DurationMs = time.Since(start).Milliseconds()
		return result
	}

	for k, v := range btn.Headers {
		req.Header.Set(k, v)
	}

	client := httpClientFor(btn)
	resp, err := client.Do(req)
	if err != nil {
		result.DurationMs = time.Since(start).Milliseconds()
		if ctx.Err() != nil {
			result.Status = "timeout"
			result.ExitCode = -1
			result.ErrorType = "TIMEOUT"
			result.Stderr = "request timed out"
			return result
		}
		result.Status = "error"
		result.ExitCode = -1
		result.ErrorType = "SCRIPT_ERROR"
		result.Stderr = fmt.Sprintf("request failed: %v", err)
		return result
	}
	defer resp.Body.Close()

	// Cap response body reads per the button's MaxResponseBytes, falling
	// back to the package-level default when the spec doesn't declare one
	// (keeps older specs working unchanged). Prevents a malicious or
	// broken endpoint from streaming an unbounded payload and OOM-ing
	// the CLI.
	limit := button.ResolveMaxResponseBytes(btn.MaxResponseBytes)
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		result.Status = "error"
		result.ExitCode = -1
		result.ErrorType = "SCRIPT_ERROR"
		result.Stderr = fmt.Sprintf("failed to read response body: %v", err)
		result.DurationMs = time.Since(start).Milliseconds()
		return result
	}

	result.DurationMs = time.Since(start).Milliseconds()
	result.Stdout = string(body)
	result.HTTPStatusCode = resp.StatusCode
	result.ExitCode = resp.StatusCode

	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		result.Status = "ok"
	} else {
		result.Status = "error"
		result.ErrorType = "SCRIPT_ERROR"
		result.Stderr = fmt.Sprintf("HTTP %d %s", resp.StatusCode, resp.Status)
	}

	return result
}

// validateHTTPTarget parses a URL after {{arg}} substitution and
// returns the canonical form ready for http.NewRequest — or an error
// if the URL shouldn't be dispatched. Runs BEFORE we build the request
// so a bad substitution can't even reach http.Transport.
//
// Defensive layer on top of newSafeDialContext: the dial hook already
// blocks connections to private IP ranges, but this function catches
// attacks earlier (bad scheme, embedded credentials, malformed URL)
// and provides a static sanitization barrier that static analyzers
// (CodeQL) recognize as a taint break.
//
// Rules:
//   - Scheme must be http or https. file://, ftp://, data:, gopher://,
//     etc. are all rejected — an HTTP button by definition makes HTTP
//     requests, so anything else is almost certainly an exfil attempt.
//   - No userinfo: URLs like https://user:pass@host/ smuggle creds
//     into the request. Refuse; the button's Headers are the right
//     place for auth.
//   - Host must be present. Empty-host URLs (http:///path) leak into
//     schemes the default resolver doesn't handle sanely.
//   - Host must parse to a valid hostname. Numeric IPv4 literals in
//     the private ranges are pre-empted here too so the error is
//     visible before a dial attempt — unless BUTTONS_ALLOW_PRIVATE_NETWORKS
//     is set, which callers already use as an escape hatch.
func validateHTTPTarget(raw string, allowPrivate bool) (string, error) {
	u, err := neturl.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("malformed URL %q: %w", raw, err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("scheme %q not allowed (only http/https)", u.Scheme)
	}
	if u.User != nil {
		return "", errors.New("URLs with embedded user:pass are not allowed; put credentials in button headers instead")
	}
	host := u.Hostname()
	if host == "" {
		return "", errors.New("URL is missing a host")
	}
	// If the host is a literal IP, short-circuit the private-range
	// check here. DNS-resolved hosts still get blocked at dial time
	// via newSafeDialContext, so this is just an early-fail path for
	// the most common smuggling case (attacker supplies a raw IP).
	if ip := net.ParseIP(host); ip != nil && !allowPrivate && !privateNetworksGloballyAllowed() {
		for _, cidr := range privateNetworks {
			if cidr.Contains(ip) {
				return "", fmt.Errorf("%s resolves to a blocked private range", ip)
			}
		}
	}
	// Strip any fragment — fragments never hit the server and their
	// presence on an HTTP request is usually a caller bug.
	u.Fragment = ""
	return u.String(), nil
}

// httpClientFor returns the http.Client to use for a given URL button.
// If the button opts in to private network access (or the env var
// override is set), we return a bare client with no SSRF protection.
// Otherwise we return a client whose transport blocks connections to
// any IP in the privateNetworks blocklist.
func httpClientFor(btn *button.Button) *http.Client {
	if btn.AllowPrivateNetworks || privateNetworksGloballyAllowed() {
		return &http.Client{}
	}
	return &http.Client{
		Transport: &http.Transport{
			DialContext: newSafeDialContext(privateNetworks),
		},
	}
}

// killProcessGroup sends SIGTERM to the process group, waits for the grace period,
// then sends SIGKILL if the process is still running.
func killProcessGroup(cmd *exec.Cmd, done <-chan error) {
	if cmd.Process == nil {
		return
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		_ = cmd.Process.Kill()
		return
	}

	_ = syscall.Kill(-pgid, syscall.SIGTERM)

	timer := time.NewTimer(sigTermGrace)
	defer timer.Stop()

	select {
	case <-done:
		return
	case <-timer.C:
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}
}
