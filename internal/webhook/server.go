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
type Server struct {
	listener net.Listener
	http     *http.Server

	mu      sync.Mutex
	waiters map[string]chan Event // correlation id -> delivery channel
	pending map[string]Event      // id -> event received before Wait() registered
}

// NewServer binds on 127.0.0.1 with an OS-picked free port and starts
// accepting. The public URL is stitched together later by the tunnel —
// this object only cares about the local side.
func NewServer() (*Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("bind local webhook listener: %w", err)
	}
	s := &Server{
		listener: ln,
		waiters:  make(map[string]chan Event),
		pending:  make(map[string]Event),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook/", s.handleWebhook)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	s.http = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = s.http.Serve(ln) }()
	return s, nil
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

// Wait blocks until a webhook for the given correlation id arrives or
// the context fires. If the event was already received before Wait was
// called, it returns immediately — this is the common case in our drawer
// flow where Register → return URL → Wait all happen in sequence.
func (s *Server) Wait(ctx context.Context, correlationID string) (Event, error) {
	s.mu.Lock()
	if ev, ok := s.pending[correlationID]; ok {
		delete(s.pending, correlationID)
		s.mu.Unlock()
		return ev, nil
	}
	ch, ok := s.waiters[correlationID]
	if !ok {
		ch = make(chan Event, 1)
		s.waiters[correlationID] = ch
	}
	s.mu.Unlock()

	select {
	case ev := <-ch:
		return ev, nil
	case <-ctx.Done():
		s.mu.Lock()
		delete(s.waiters, correlationID)
		s.mu.Unlock()
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
	if ch, ok := s.waiters[id]; ok {
		delete(s.waiters, id)
		s.mu.Unlock()
		// Non-blocking send; buffer of 1 guarantees success.
		ch <- ev
	} else {
		// Stash for a Wait() that hasn't landed yet.
		s.pending[id] = ev
		s.mu.Unlock()
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// ErrAlreadyRegistered is returned when the same correlation id is
// reused within a process. Shouldn't happen in practice because we mint
// fresh ids, but guarded so duplicate ids can't collide silently.
var ErrAlreadyRegistered = errors.New("correlation id already registered")
