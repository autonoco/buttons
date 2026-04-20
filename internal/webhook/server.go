package webhook

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Event is one webhook POST captured from an upstream service. Headers
// are trimmed to the small set we care about so a large Authorization
// value doesn't bleed into drawer history.
type Event struct {
	Method    string            `json:"method"`
	Path      string            `json:"path"`
	Headers   map[string]string `json:"headers"`
	Body      json.RawMessage   `json:"body"`
	ReceivedAt time.Time        `json:"received_at"`
}

// Server is the local HTTP receiver for webhook POSTs. One listener per
// process is enough — we multiplex by correlation id embedded in the
// URL path (/webhook/<id>).
//
// readyToken is a per-process random secret embedded in the /healthz
// response. The tunnel readiness check fetches /healthz through the
// public URL and matches the token — this closes the loop through
// edge DNS + CF → local process and rejects stale DNS pointing
// somewhere else that happens to return 2xx.
type Server struct {
	listener   net.Listener
	http       *http.Server
	readyToken string

	mu      sync.Mutex
	waiters map[string]chan Event // correlation id -> delivery channel; callers Register before publishing URL
}

// NewServer binds on 127.0.0.1 with an OS-picked free port and starts
// accepting. The public URL is stitched together later by the tunnel —
// this object only cares about the local side.
func NewServer() (*Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("bind local webhook listener: %w", err)
	}
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("mint readiness token: %w", err)
	}
	s := &Server{
		listener:   ln,
		readyToken: hex.EncodeToString(tokenBytes),
		waiters:    make(map[string]chan Event),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook/", s.handleWebhook)
	mux.HandleFunc("/healthz", s.handleHealthz)
	s.http = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = s.http.Serve(ln) }()
	return s, nil
}

// ReadyToken returns the process-local secret embedded in /healthz
// responses. Callers pass it to tunnel readiness checks so a 2xx from
// a hostname pointing at some unrelated origin isn't accepted.
func (s *Server) ReadyToken() string {
	return s.readyToken
}

// handleHealthz serves the readiness token in JSON. Kept separate
// from the webhook handler so the token surface is minimal and
// obvious at a glance. CORS not needed — only cloudflared and the
// drawer dispatcher hit this endpoint.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true,"token":"` + s.readyToken + `"}`))
}

// Port returns the randomly-assigned local port.
func (s *Server) Port() int {
	return s.listener.Addr().(*net.TCPAddr).Port
}

// LocalURL is what we hand cloudflared as its upstream.
func (s *Server) LocalURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", s.Port())
}

// NewCorrelationID returns a random unguessable id. Used as path suffix
// so a third party who guesses the public hostname still can't
// enumerate other pending webhooks.
func NewCorrelationID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("random correlation id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// Register creates a waiter channel for the given correlation id and
// returns it. Callers must call Register BEFORE making the external
// URL publicly available (e.g. before spawning the POST goroutine in
// `webhook test`); that way the handler always finds a waiter when
// the event arrives and we don't need an unbounded pending-map stash.
// The returned channel is buffered (size 1) so handler delivery
// always succeeds without blocking.
//
// Caller is responsible for calling Deregister (e.g. via defer) if
// the waiter is never consumed — on ctx cancel, timeout, etc. Without
// that we'd leak one map entry per abandoned Wait.
func (s *Server) Register(correlationID string) <-chan Event {
	ch := make(chan Event, 1)
	s.mu.Lock()
	s.waiters[correlationID] = ch
	s.mu.Unlock()
	return ch
}

// Deregister removes a correlation id's waiter entry. Safe to call
// even if the waiter was already consumed (idempotent).
func (s *Server) Deregister(correlationID string) {
	s.mu.Lock()
	delete(s.waiters, correlationID)
	s.mu.Unlock()
}

// Wait is a convenience helper: Register + block on the returned
// channel or ctx.Done, with Deregister on exit. Matches the previous
// Wait() signature so existing callers compile unchanged — they just
// get the leak-free Register-first semantics.
func (s *Server) Wait(ctx context.Context, correlationID string) (Event, error) {
	ch := s.Register(correlationID)
	defer s.Deregister(correlationID)
	select {
	case ev := <-ch:
		return ev, nil
	case <-ctx.Done():
		return Event{}, ctx.Err()
	}
}

// Close stops the listener. Pending waiters observe a closed server via
// their context (the executor is responsible for passing a cancellable
// ctx to Wait).
func (s *Server) Close() error {
	if s.http == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.http.Shutdown(ctx)
}

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/webhook/")
	if id == "" || strings.Contains(id, "/") {
		http.Error(w, "bad correlation id", http.StatusBadRequest)
		return
	}

	// Cap body at 1 MiB — webhook payloads over this are a misuse of
	// the channel; the real data lives wherever the service stored it
	// (dataset, object store, etc.).
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	_ = r.Body.Close()

	var raw json.RawMessage = body
	if len(body) == 0 {
		raw = json.RawMessage("null")
	} else if !json.Valid(body) {
		// Store non-JSON as a quoted string so downstream JSON
		// consumers don't choke. Most services post JSON; the ones
		// that don't still deserve to be observable.
		quoted, _ := json.Marshal(string(body))
		raw = quoted
	}

	headers := map[string]string{}
	for _, k := range []string{"Content-Type", "User-Agent", "X-Forwarded-For"} {
		if v := r.Header.Get(k); v != "" {
			headers[k] = v
		}
	}
	ev := Event{
		Method:     r.Method,
		Path:       r.URL.Path,
		Headers:    headers,
		Body:       raw,
		ReceivedAt: time.Now().UTC(),
	}

	s.mu.Lock()
	ch, ok := s.waiters[id]
	if ok {
		delete(s.waiters, id)
	}
	s.mu.Unlock()
	if ok {
		// Non-blocking send; buffer of 1 guarantees success.
		ch <- ev
	}
	// No waiter registered → drop. Callers must Register before
	// publishing the URL so this path should be rare; if it happens
	// it means a stray POST arrived outside any active flow.

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// ErrAlreadyRegistered is returned when the same correlation id is
// reused within a process. Shouldn't happen in practice because we mint
// fresh ids, but guarded so duplicate ids can't collide silently.
var ErrAlreadyRegistered = errors.New("correlation id already registered")
