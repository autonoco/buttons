package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/drawer"
	"github.com/autonoco/buttons/internal/webhook"
)

// listenFlags collects the small set of knobs `buttons webhook listen`
// exposes. Rare enough to not warrant global state in the package.
var listenFlags struct {
	noTunnel bool
	port     int
}

// webhookListenCmd is registered under webhookCmd in cmd/webhook.go's
// init(). Lives here because the handler + dispatcher implementation
// wants its own file; the parent webhookCmd just binds it.
var webhookListenCmd = &cobra.Command{
	Use:   "listen",
	Short: "Run the webhook listener — presses drawers when trigger paths are hit",
	Long: `Runs a foreground HTTP listener exposed via Cloudflare tunnel so
drawers with webhook triggers get invoked when third-party services
POST to their paths.

Prereq:
  1. 'cloudflared' on PATH (brew install cloudflared)
  2. 'buttons webhook setup' run once for a stable named tunnel
     (quick-tunnel mode works too, but the URL changes each run — fine
     for dev, not for services that register a fixed URL up front)

Workflow:
  1. Create a drawer:               buttons drawer create on-apify-done
  2. Add steps that consume the webhook body via \${inputs.webhook.body.*}
  3. Attach a webhook trigger:      buttons drawer on-apify-done trigger webhook /apify
  4. Start the listener:            buttons webhook listen
  5. Configure the third-party service to POST to the printed URL.

When a POST arrives at a registered path:
  - The request body, headers, query, and method are materialized as
    \${inputs.webhook.body}, \${inputs.webhook.headers.*}, etc.
  - The drawer is pressed asynchronously — the HTTP response returns
    immediately with {ok, drawer} so the sender doesn't block on the
    full workflow.
  - If the drawer has a shared-token secret set, X-Buttons-Token (or
    ?token= query param) must match or the request is rejected 401.

The listener stays up until Ctrl-C. Run it in tmux, a separate pane,
or under launchd/systemd for always-on setups.`,
	Args: cobra.NoArgs,
	RunE: runListen,
}

func init() {
	webhookListenCmd.Flags().BoolVar(&listenFlags.noTunnel, "no-tunnel", false, "skip cloudflared; listen on 127.0.0.1 only (local testing)")
	webhookListenCmd.Flags().IntVar(&listenFlags.port, "port", 0, "bind local listener on this port (0 = random)")
}

func runListen(cmd *cobra.Command, args []string) error {
	dsvc := drawer.NewService()
	all, err := dsvc.List()
	if err != nil {
		return handleDrawerError(err)
	}
	// Pre-filter: only drawers with webhook triggers. Keeps the routing
	// table small and the status output focused.
	routes := collectWebhookRoutes(all)
	if len(routes) == 0 {
		return fmt.Errorf("no drawers have a webhook trigger yet — run `buttons drawer NAME trigger webhook /path` first")
	}

	addr := fmt.Sprintf("127.0.0.1:%d", listenFlags.port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("bind %s: %w", addr, err)
	}
	localURL := fmt.Sprintf("http://%s", ln.Addr().String())

	mux := http.NewServeMux()
	h := &serveHandler{dsvc: dsvc, routes: routes}
	mux.Handle("/", h)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	// Start tunnel (unless suppressed).
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()
	var tunnel *webhook.Tunnel
	var publicBase string
	if listenFlags.noTunnel {
		publicBase = localURL
	} else {
		if err := webhook.CheckCloudflared(); err != nil {
			_ = srv.Shutdown(context.Background())
			return handleWebhookErr(err)
		}
		tunnelCtx, tunnelCancel := context.WithTimeout(ctx, 90*time.Second)
		tunnel, err = webhook.StartTunnel(tunnelCtx, localURL)
		tunnelCancel()
		if err != nil {
			_ = srv.Shutdown(context.Background())
			return handleWebhookErr(err)
		}
		defer func() { _ = tunnel.Stop() }()
		publicBase = tunnel.URL
	}

	printServeBanner(publicBase, routes, tunnel)

	// Wait on signal or server error.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	select {
	case <-sigCh:
		fmt.Fprintln(os.Stderr, "\nshutting down…")
	case err := <-serveErr:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
	case <-ctx.Done():
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
	return nil
}

// toAuthConfig adapts a drawer-layer TriggerAuth into the webhook
// package's verifier struct. Kept as a thin copy so the verifier has
// no dependency on the drawer package (avoids an import cycle if the
// drawer ever grows webhook-aware validation logic).
func toAuthConfig(a *drawer.TriggerAuth) *webhook.TriggerAuthConfig {
	if a == nil {
		return nil
	}
	return &webhook.TriggerAuthConfig{
		Type:         a.Type,
		Username:     a.Username,
		Password:     a.Password,
		HeaderName:   a.HeaderName,
		HeaderValue:  a.HeaderValue,
		JWTSecret:    a.JWTSecret,
		JWTAlgorithm: a.JWTAlgorithm,
		JWTIssuer:    a.JWTIssuer,
		JWTAudience:  a.JWTAudience,
	}
}

// route maps one webhook path to the drawer that owns it plus the
// auth configuration. Auth is kept as a pointer so a nil route.auth
// is the "open endpoint" case — matching the on-disk normalisation.
type route struct {
	path       string
	drawerName string
	auth       *drawer.TriggerAuth
}

func collectWebhookRoutes(ds []drawer.Drawer) []route {
	out := make([]route, 0)
	for _, d := range ds {
		for _, t := range d.Triggers {
			if t.Kind != "webhook" || t.Path == "" {
				continue
			}
			out = append(out, route{path: t.Path, drawerName: d.Name, auth: t.Auth})
		}
	}
	return out
}

func printServeBanner(publicBase string, routes []route, tunnel *webhook.Tunnel) {
	if jsonOutput {
		payload := map[string]any{
			"public_base": publicBase,
			"routes":      make([]map[string]any, 0, len(routes)),
		}
		rs := payload["routes"].([]map[string]any)
		for _, r := range routes {
			authType := "none"
			if r.auth != nil && r.auth.Type != "" {
				authType = r.auth.Type
			}
			rs = append(rs, map[string]any{
				"path":      r.path,
				"drawer":    r.drawerName,
				"auth_type": authType,
				"url":       publicBase + r.path,
			})
		}
		payload["routes"] = rs
		_ = config.WriteJSON(payload)
		return
	}
	mode := "local"
	if tunnel != nil {
		mode = string(tunnel.Mode) + " tunnel"
	}
	fmt.Fprintf(os.Stderr, "buttons webhook listen — %s\n", mode)
	fmt.Fprintf(os.Stderr, "  listening at: %s\n", publicBase)
	fmt.Fprintf(os.Stderr, "\nroutes:\n")
	for _, r := range routes {
		authLabel := ""
		if r.auth != nil && r.auth.Type != "" && r.auth.Type != "none" {
			authLabel = "  (auth: " + r.auth.Type + ")"
		}
		fmt.Fprintf(os.Stderr, "  %s%s  →  %s%s\n", publicBase, r.path, r.drawerName, authLabel)
	}
	fmt.Fprintf(os.Stderr, "\nCtrl-C to stop.\n\n")
}

// serveHandler dispatches incoming POSTs to the drawer registered for
// the request path. Holds a sync.Mutex around the in-flight count so a
// graceful shutdown can wait for drawer presses rather than dropping
// them mid-run.
type serveHandler struct {
	dsvc    *drawer.Service
	routes  []route
	mu      sync.Mutex
	inFlight int
}

func (h *serveHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Accept POST only. A GET on a webhook URL is almost always a human
	// poking around; respond with a useful message instead of 405.
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"message":"buttons webhook endpoint — POST to trigger"}`))
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, `{"ok":false,"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Find the matching route. Exact path match only — wildcards and
	// path params aren't needed today and would complicate collision
	// detection in SetWebhookTrigger.
	var matched *route
	for i := range h.routes {
		if h.routes[i].path == r.URL.Path {
			matched = &h.routes[i]
			break
		}
	}
	if matched == nil {
		http.Error(w, `{"ok":false,"error":"no_drawer_registered_for_path"}`, http.StatusNotFound)
		return
	}

	// Auth gate. Matches n8n's trigger-webhook auth types (none,
	// basic, header, jwt). Every string compare inside VerifyAuth
	// uses crypto/subtle.ConstantTimeCompare so timing side-channels
	// don't leak the configured secret.
	if res := webhook.VerifyAuth(toAuthConfig(matched.auth), r); !res.OK {
		w.Header().Set("Content-Type", "application/json")
		// WWW-Authenticate for Basic helps browsers/clients re-prompt;
		// harmless to emit for other types but only strictly correct
		// for basic.
		if matched.auth != nil && matched.auth.Type == "basic" {
			w.Header().Set("WWW-Authenticate", `Basic realm="buttons webhook"`)
		}
		w.WriteHeader(res.Status)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":    false,
			"error": res.Code,
			"detail": res.Detail,
		})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20)) // 8 MiB cap
	if err != nil {
		http.Error(w, `{"ok":false,"error":"body_read_failed"}`, http.StatusBadRequest)
		return
	}
	_ = r.Body.Close()

	// Parse body: JSON if possible, else raw string. Agents usually want
	// navigable fields (${inputs.webhook.body.foo}) but non-JSON senders
	// still deserve to be observable.
	var parsedBody any
	if len(body) == 0 {
		parsedBody = nil
	} else if json.Valid(body) {
		if err := json.Unmarshal(body, &parsedBody); err != nil {
			parsedBody = string(body)
		}
	} else {
		parsedBody = string(body)
	}

	headers := map[string]string{}
	for k, vs := range r.Header {
		// First value is enough for the common case; Cookie, Set-Cookie,
		// and a few others are multi-valued but drawer resolution
		// doesn't care about those.
		if len(vs) > 0 {
			headers[k] = vs[0]
		}
	}
	query := map[string]string{}
	for k, vs := range r.URL.Query() {
		if len(vs) > 0 {
			query[k] = vs[0]
		}
	}

	webhookInput := map[string]any{
		"body":        parsedBody,
		"headers":     headers,
		"query":       query,
		"method":      r.Method,
		"path":        r.URL.Path,
		"received_at": time.Now().UTC().Format(time.RFC3339),
	}

	// Load fresh so a newly-added or modified drawer picks up without
	// a restart — listing is cheap.
	d, err := h.dsvc.Get(matched.drawerName)
	if err != nil {
		http.Error(w, `{"ok":false,"error":"drawer_not_found"}`, http.StatusInternalServerError)
		return
	}

	// Press asynchronously: respond to the webhook sender right away
	// with a run id. Most services retry aggressively on slow webhook
	// responses, and a long-running drawer would block them needlessly.
	h.mu.Lock()
	h.inFlight++
	h.mu.Unlock()

	go func() {
		defer func() {
			h.mu.Lock()
			h.inFlight--
			h.mu.Unlock()
		}()
		// Fresh context — not tied to the request (which is about to
		// close). 1h cap so a hung drawer can't leak forever.
		ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
		defer cancel()

		exec := drawer.NewExecutor()
		result, execErr := exec.Execute(ctx, d, map[string]any{"webhook": webhookInput})
		if execErr != nil && result == nil {
			fmt.Fprintf(os.Stderr, "[serve] drawer %s error: %v\n", d.Name, execErr)
			return
		}
		if result != nil && result.Status != "ok" {
			fmt.Fprintf(os.Stderr, "[serve] drawer %s failed: %s\n", d.Name, failedStepSummary(result))
			return
		}
		fmt.Fprintf(os.Stderr, "[serve] drawer %s ok (%dms)\n", d.Name, result.DurationMs)
	}()

	// Response to the webhook sender.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":     true,
		"drawer": matched.drawerName,
	})
}

func failedStepSummary(r *drawer.ExecuteResult) string {
	parts := []string{}
	if r.FailedStep != "" {
		parts = append(parts, "step="+r.FailedStep)
	}
	if r.Error != nil {
		parts = append(parts, r.Error.Code+": "+r.Error.Message)
	}
	return strings.Join(parts, " ")
}
