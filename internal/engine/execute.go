package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
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
func Execute(ctx context.Context, btn *button.Button, args map[string]string, codePath string) *Result {
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

	// Build environment: inherit current + button env + args as BUTTONS_ARG_<NAME>
	env := os.Environ()
	for k, v := range btn.Env {
		env = append(env, k+"="+v)
	}
	for k, v := range args {
		envName := "BUTTONS_ARG_" + strings.ToUpper(strings.ReplaceAll(k, "-", "_"))
		env = append(env, envName+"="+v)
	}
	cmd.Env = env

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

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
	url := SubstituteURL(btn.URL, args)

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

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
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
