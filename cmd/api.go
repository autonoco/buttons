package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/autonoco/buttons/internal/apiserver"
	"github.com/autonoco/buttons/internal/config"
	"github.com/spf13/cobra"
)

var (
	servePort        int
	serveHost        string
	serveAPIKey      string
	serveAllowHTTPBt bool
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run a REST API server exposing buttons over HTTP",
	Long: `Start an HTTP server that exposes your buttons as a REST API, using the
same structured-output contract and execution path as the CLI.

Endpoints:
  GET  /api/health               liveness (no auth)
  GET  /api/buttons              list all buttons
  GET  /api/buttons/{name}       a button's spec
  POST /api/buttons/{name}/press execute a button (JSON body: {"args":{…},"timeout":N})
  GET  /api/buttons/{name}/runs  recent run history

Auth: every endpoint except /api/health requires 'Authorization: Bearer <key>'.
The key comes from --api-key, else the 'API_KEY' battery, else $BUTTONS_API_KEY.
With no key the server runs auth-free and therefore binds to loopback only —
binding a non-loopback host without a key is refused.

Buttons created via the CLI while the server runs are picked up immediately
(state is read from disk per request).

Examples:
  buttons serve
  buttons serve --port 3000
  buttons serve --host 0.0.0.0 --api-key "$(openssl rand -hex 16)"`,
	Args: cobra.NoArgs,
	RunE: runServe,
}

func runServe(cmd *cobra.Command, _ []string) error {
	apiKey := serveAPIKey
	if apiKey == "" {
		apiKey = resolveServeAPIKey()
	}

	host := serveHost
	if host == "" {
		host = "127.0.0.1"
	}

	// Safety: refuse to expose an unauthenticated server beyond loopback.
	if apiKey == "" && !isLoopbackHost(host) {
		msg := fmt.Sprintf("refusing to bind %s without an API key — set --api-key, the 'API_KEY' battery, or $BUTTONS_API_KEY (or bind 127.0.0.1)", host)
		if jsonOutput {
			_ = config.WriteJSONError("VALIDATION_ERROR", msg)
			return errSilent
		}
		return fmt.Errorf("%s", msg)
	}

	addr := fmt.Sprintf("%s:%d", host, servePort)
	srv := apiserver.New(apiserver.Config{APIKey: apiKey, AllowHTTPButtons: serveAllowHTTPBt})

	if jsonOutput {
		_ = config.WriteJSON(map[string]any{
			"addr": addr,
			"auth": apiKey != "",
			"endpoints": []string{
				"GET /api/health",
				"GET /api/buttons",
				"GET /api/buttons/{name}",
				"POST /api/buttons/{name}/press",
				"GET /api/buttons/{name}/runs",
			},
		})
	} else {
		fmt.Fprintln(os.Stderr, srv.Describe(addr))
		if apiKey == "" {
			fmt.Fprintln(os.Stderr, "  ⚠ no API key set — auth disabled (loopback only)")
		}
		fmt.Fprintln(os.Stderr, "  Ctrl-C to stop.")
	}

	// Shutdown on signal or parent context cancel.
	ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	shutdown := make(chan struct{})
	go func() {
		<-ctx.Done()
		close(shutdown)
	}()

	if err := srv.ListenAndServe(addr, shutdown); err != nil {
		if jsonOutput {
			_ = config.WriteJSONError("INTERNAL_ERROR", err.Error())
			return errSilent
		}
		return err
	}
	return nil
}

// resolveServeAPIKey pulls the server key from the 'API_KEY' battery, falling
// back to $BUTTONS_API_KEY. Batteries keep the key off the command line and
// out of shell history.
func resolveServeAPIKey() string {
	if batSvc, err := newBatteryService(); err == nil {
		if v, _, gerr := batSvc.Get("API_KEY"); gerr == nil && v != "" {
			return v
		}
	}
	return os.Getenv("BUTTONS_API_KEY")
}

func isLoopbackHost(host string) bool {
	switch host {
	case "127.0.0.1", "::1", "localhost":
		return true
	}
	return strings.HasPrefix(host, "127.")
}

func init() {
	serveCmd.Flags().IntVar(&servePort, "port", 8080, "port to listen on")
	serveCmd.Flags().StringVar(&serveHost, "host", "127.0.0.1", "host/interface to bind (use 0.0.0.0 to expose; requires an API key)")
	serveCmd.Flags().StringVar(&serveAPIKey, "api-key", "", "bearer key required on all endpoints (else the 'API_KEY' battery or $BUTTONS_API_KEY)")
	serveCmd.Flags().BoolVar(&serveAllowHTTPBt, "allow-http-buttons", false, "allow pressing http (outbound-request) buttons over the API (SSRF surface; off by default)")
	rootCmd.AddCommand(serveCmd)
}
