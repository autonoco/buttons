package cmd

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/autonoco/buttons/internal/battery"
	"github.com/spf13/cobra"
)

// buttons login — browser-based authorization against the Buttons platform
// (PKCE authorization-code with a 127.0.0.1 loopback redirect, the same shape
// as `gh auth login`). The browser page mints nothing by itself: this process
// holds the PKCE verifier, so only it can redeem the one-time code for the
// publish token. The token lands in the global REGISTRY_WRITE_KEY battery,
// which `buttons publish` already reads — no separate credential store.

const (
	defaultRegistryURL = "https://api.buttons.sh"
	defaultDeskURL     = "https://desk.buttons.sh"
	loginTimeout       = 3 * time.Minute
)

var (
	loginRegistry  string
	loginDesk      string
	loginNoBrowser bool
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Connect this machine to the Buttons platform",
	Long: `Authorize this machine in your browser and store a publish token.

Opens your Buttons console to approve the connection under your organization,
then stores the issued token as the global REGISTRY_WRITE_KEY battery (used by
"buttons publish") and pins the registry URL as the REGISTRY_URL battery.

Revoke a machine any time from the console's Desks page.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		verifier, challenge, state, err := pkcePair()
		if err != nil {
			return fmt.Errorf("generate PKCE material: %w", err)
		}

		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return fmt.Errorf("start loopback listener: %w", err)
		}
		defer listener.Close()
		port := listener.Addr().(*net.TCPAddr).Port

		host, _ := os.Hostname()
		authorizeURL := strings.TrimRight(loginDesk, "/") + "/cli-authorize?" + url.Values{
			"state":     {state},
			"challenge": {challenge},
			"port":      {fmt.Sprint(port)},
			"label":     {host},
		}.Encode()

		if loginNoBrowser || !openBrowser(authorizeURL) {
			fmt.Fprintf(os.Stderr, "Open this URL in your browser to authorize:\n\n  %s\n\n", authorizeURL)
		} else {
			fmt.Fprintln(os.Stderr, "Opened your browser to authorize this machine. Waiting…")
		}

		code, err := waitForLoopbackCode(listener, state)
		if err != nil {
			return err
		}

		token, orgID, err := exchangeLoginCode(strings.TrimRight(loginRegistry, "/"), code, verifier)
		if err != nil {
			return err
		}

		svc, err := newBatteryService()
		if err != nil {
			return handleBatteryError(err)
		}
		if err := svc.Set("REGISTRY_WRITE_KEY", token, battery.ScopeGlobal); err != nil {
			return handleBatteryError(err)
		}
		if err := svc.Set("REGISTRY_URL", strings.TrimRight(loginRegistry, "/"), battery.ScopeGlobal); err != nil {
			return handleBatteryError(err)
		}

		fmt.Fprintf(os.Stderr, "Logged in: %s connected to %s\n", host, orgID)
		printNextHint("publish with `buttons publish @%s/<name>`", orgID)
		return nil
	},
}

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Disconnect this machine from the Buttons platform",
	Long: `Remove the stored publish token and registry URL.

The token itself stays valid until revoked from the console's Desks page —
logout only forgets it on this machine.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newBatteryService()
		if err != nil {
			return handleBatteryError(err)
		}
		for _, key := range []string{"REGISTRY_WRITE_KEY", "REGISTRY_URL"} {
			if err := svc.Delete(key, battery.ScopeGlobal); err != nil {
				return handleBatteryError(err)
			}
		}
		fmt.Fprintln(os.Stderr, "Logged out. Revoke the token from the console's Desks page to invalidate it.")
		return nil
	},
}

// pkcePair returns (verifier, S256 challenge, state), all base64url.
func pkcePair() (string, string, string, error) {
	random := func(bytes int) (string, error) {
		buf := make([]byte, bytes)
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		return base64.RawURLEncoding.EncodeToString(buf), nil
	}
	verifier, err := random(32)
	if err != nil {
		return "", "", "", err
	}
	state, err := random(16)
	if err != nil {
		return "", "", "", err
	}
	digest := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(digest[:]), state, nil
}

// waitForLoopbackCode serves exactly one /callback on the loopback listener and
// returns the delivered code once its state matches ours.
func waitForLoopbackCode(listener net.Listener, state string) (string, error) {
	type result struct {
		code string
		err  error
	}
	results := make(chan result, 1)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback" {
			http.NotFound(w, r)
			return
		}
		query := r.URL.Query()
		switch {
		case query.Get("state") != state:
			http.Error(w, "state mismatch", http.StatusBadRequest)
			results <- result{err: errors.New("login failed: state mismatch on loopback callback")}
		case query.Get("error") != "":
			fmt.Fprintln(w, "Authorization denied. You can close this tab.")
			results <- result{err: fmt.Errorf("login denied in browser: %s", query.Get("error"))}
		case query.Get("code") == "":
			http.Error(w, "missing code", http.StatusBadRequest)
			results <- result{err: errors.New("login failed: loopback callback carried no code")}
		default:
			fmt.Fprintln(w, "Machine authorized. You can close this tab and return to the terminal.")
			results <- result{code: query.Get("code")}
		}
	})}
	go func() { _ = server.Serve(listener) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()

	select {
	case r := <-results:
		return r.code, r.err
	case <-time.After(loginTimeout):
		return "", errors.New("login timed out waiting for the browser authorization")
	}
}

// exchangeLoginCode redeems the one-time code + PKCE verifier for the token.
func exchangeLoginCode(registry, code, verifier string) (token, orgID string, err error) {
	body, err := json.Marshal(map[string]string{"code": code, "verifier": verifier})
	if err != nil {
		return "", "", err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	response, err := client.Post(registry+"/v1/cli-auth/exchange", "application/json", strings.NewReader(string(body)))
	if err != nil {
		return "", "", fmt.Errorf("registry %s: %w", registry, err)
	}
	defer response.Body.Close()
	var payload struct {
		Token string `json:"token"`
		OrgID string `json:"org_id"`
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return "", "", fmt.Errorf("registry %s: malformed exchange response: %w", registry, err)
	}
	if response.StatusCode != http.StatusOK || payload.Token == "" {
		code := "exchange failed"
		if payload.Error != nil {
			code = payload.Error.Code
		}
		return "", "", fmt.Errorf("login failed: %s (HTTP %d)", code, response.StatusCode)
	}
	return payload.Token, payload.OrgID, nil
}

// openBrowser best-effort opens the URL; false means the caller should print it.
func openBrowser(target string) bool {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", target)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		command = exec.Command("xdg-open", target)
	}
	return command.Start() == nil
}

func init() {
	loginCmd.Flags().StringVar(&loginRegistry, "registry", envOr("BUTTONS_REGISTRY_URL", defaultRegistryURL), "registry base URL the token is issued for")
	loginCmd.Flags().StringVar(&loginDesk, "desk", envOr("BUTTONS_DESK_URL", defaultDeskURL), "Buttons console URL that hosts the authorization page")
	loginCmd.Flags().BoolVar(&loginNoBrowser, "no-browser", false, "print the authorization URL instead of opening a browser")
	rootCmd.AddCommand(loginCmd)
	rootCmd.AddCommand(logoutCmd)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
