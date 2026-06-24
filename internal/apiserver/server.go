// Package apiserver exposes buttons over HTTP as a small REST API (#270).
// It reuses internal/runner so presses go through the exact same validate →
// batteries → queue → execute → record path as the CLI, and matches the CLI's
// {ok,data}/{ok,error} JSON envelope. No web framework — stdlib net/http.
package apiserver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/engine"
	"github.com/autonoco/buttons/internal/history"
	"github.com/autonoco/buttons/internal/runner"
)

// Config configures a Server.
type Config struct {
	// APIKey, when non-empty, is required as `Authorization: Bearer <key>` on
	// every endpoint except /api/health. Empty disables auth (loopback dev).
	APIKey string
	// MaxBodyBytes caps a press request body. 0 → 1 MiB default.
	MaxBodyBytes int64
	// AllowHTTPButtons permits pressing runtime:"http" buttons over the REST
	// API. Off by default: an HTTP button makes an outbound request whose
	// path/query can carry request-supplied args, so exposing it to network
	// callers is an SSRF surface (the host is locked at create time, but we
	// gate it anyway). Shell/code/prompt buttons are always pressable.
	AllowHTTPButtons bool
	// MaxTimeoutSeconds caps the effective press timeout regardless of the
	// per-request `timeout` or the button's own setting. 0 → 300.
	MaxTimeoutSeconds int
	// MaxConcurrentPresses bounds in-flight presses across the API; excess
	// requests get 503. 0 → 8. A button's own Queue still applies on top.
	MaxConcurrentPresses int
}

// Server is the buttons REST handler. Construct with New and mount its Handler.
type Server struct {
	cfg Config
	svc *button.Service
	mux *http.ServeMux
	sem chan struct{} // bounds concurrent presses
}

// New builds a Server. Routes are registered once; button state is read from
// disk per request, so buttons created via the CLI while the server runs are
// visible immediately (AC: "New buttons created via CLI are visible").
func New(cfg Config) *Server {
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = 1 << 20
	}
	if cfg.MaxTimeoutSeconds <= 0 {
		cfg.MaxTimeoutSeconds = 300
	}
	if cfg.MaxConcurrentPresses <= 0 {
		cfg.MaxConcurrentPresses = 8
	}
	s := &Server{
		cfg: cfg,
		svc: button.NewService(),
		mux: http.NewServeMux(),
		sem: make(chan struct{}, cfg.MaxConcurrentPresses),
	}
	s.routes()
	return s
}

// Handler returns the http.Handler for mounting (also used directly in tests).
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/buttons", s.auth(s.handleList))
	s.mux.HandleFunc("GET /api/buttons/{name}", s.auth(s.handleGet))
	s.mux.HandleFunc("POST /api/buttons/{name}/press", s.auth(s.handlePress))
	s.mux.HandleFunc("GET /api/buttons/{name}/runs", s.auth(s.handleRuns))
}

// auth wraps a handler with bearer-key enforcement (constant-time compare).
// Skipped entirely when no key is configured.
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.APIKey != "" && !s.authorized(r) {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing or invalid bearer token")
			return
		}
		next(w, r)
	}
}

func (s *Server) authorized(r *http.Request) bool {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, prefix) {
		return false
	}
	got := strings.TrimSpace(h[len(prefix):])
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg.APIKey)) == 1
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeData(w, http.StatusOK, map[string]any{"status": "ok", "service": "buttons"})
}

func (s *Server) handleList(w http.ResponseWriter, _ *http.Request) {
	buttons, err := s.svc.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	writeData(w, http.StatusOK, map[string]any{"buttons": buttons})
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	btn, err := s.svc.Get(name)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeData(w, http.StatusOK, btn)
}

func (s *Server) handleRuns(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if _, err := s.svc.Get(name); err != nil {
		writeServiceError(w, err)
		return
	}
	runs, err := history.List(name, 20)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if runs == nil {
		runs = []history.Run{}
	}
	writeData(w, http.StatusOK, map[string]any{"runs": runs})
}

// pressRequest is the POST body for a press: {"args": {...}, "timeout": 30}.
type pressRequest struct {
	Args    map[string]string `json:"args"`
	Timeout int               `json:"timeout"`
}

func (s *Server) handlePress(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	var req pressRequest
	if r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxBodyBytes)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			var mbe *http.MaxBytesError
			if errors.As(err, &mbe) {
				writeError(w, http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE", "request body exceeds limit")
				return
			}
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body")
			return
		}
		if len(strings.TrimSpace(string(body))) > 0 {
			if err := json.Unmarshal(body, &req); err != nil {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body: "+err.Error())
				return
			}
		}
	}

	// Gate HTTP buttons: pressing one makes an outbound request whose
	// path/query carry request-supplied args. The host is locked at create
	// time (no arbitrary-host SSRF), but exposing the outbound request to
	// network callers is still opt-in. Shell/code/prompt buttons are unaffected.
	if !s.cfg.AllowHTTPButtons {
		if btn, gerr := s.svc.Get(name); gerr == nil && btn.Runtime == "http" {
			writeError(w, http.StatusForbidden, "HTTP_BUTTON_BLOCKED",
				"pressing http buttons over the API is disabled; start `buttons serve --allow-http-buttons` to enable")
			return
		}
	}

	// Bound concurrent presses — this path executes shell/code. Excess → 503.
	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	default:
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusServiceUnavailable, "BUSY", "too many concurrent presses; retry shortly")
		return
	}

	result, err := runner.Press(r.Context(), name, req.Args, runner.Options{
		TimeoutSeconds:    req.Timeout,
		MaxTimeoutSeconds: s.cfg.MaxTimeoutSeconds,
		RecordHistory:     true,
	})
	if err != nil {
		// Map pre-flight failures (missing button, bad args) to 4xx.
		writePressPreflightError(w, err)
		return
	}

	// A button that executed (even non-zero exit) is a 200 with the result;
	// callers branch on result.status, mirroring the CLI's --json contract.
	writeData(w, http.StatusOK, result)
}

// writePressPreflightError classifies a runner.Press pre-flight error.
func writePressPreflightError(w http.ResponseWriter, err error) {
	var se *button.ServiceError
	if errors.As(err, &se) {
		status := http.StatusBadRequest
		if se.Code == "NOT_FOUND" {
			status = http.StatusNotFound
		}
		writeError(w, status, se.Code, se.Message)
		return
	}
	writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
}

func writeServiceError(w http.ResponseWriter, err error) {
	var se *button.ServiceError
	if errors.As(err, &se) {
		status := http.StatusBadRequest
		if se.Code == "NOT_FOUND" {
			status = http.StatusNotFound
		}
		writeError(w, status, se.Code, se.Message)
		return
	}
	writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
}

// writeData emits the CLI's success envelope: {"ok":true,"data":…}.
func writeData(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": data})
}

// writeError emits the CLI's error envelope: {"ok":false,"error":{code,message}}.
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":    false,
		"error": map[string]string{"code": code, "message": message},
	})
}

// Ensure engine.Result stays JSON-encodable through this package (compile-time
// nudge so a field rename there surfaces here).
var _ = engine.Result{}

// ListenAndServe starts the server on addr with sane timeouts and blocks until
// the context is cancelled, then drains within 5s.
func (s *Server) ListenAndServe(addr string, shutdown <-chan struct{}) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-shutdown:
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	}
}

// describe is a tiny helper for the startup banner.
func (s *Server) describe(addr string) string {
	auth := "no auth (loopback dev)"
	if s.cfg.APIKey != "" {
		auth = "bearer-key required"
	}
	return fmt.Sprintf("buttons serve on http://%s — %s", addr, auth)
}

// Describe exposes the banner string for the command layer.
func (s *Server) Describe(addr string) string { return s.describe(addr) }
