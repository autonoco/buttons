// Package mcpserver implements `buttons mcp` — a Model Context Protocol server
// over stdio (newline-delimited JSON-RPC 2.0). It exposes buttons to agents via
// a thin *meta-tool* surface (buttons_list / buttons_press / buttons_inspect,
// and optionally buttons_create) rather than one MCP tool per button, so large
// button sets don't degrade MCP clients that struggle past a few dozen tools.
//
// Protocol notes: MCP's stdio transport frames each JSON-RPC message on its own
// line — messages MUST NOT contain embedded newlines. stdout therefore carries
// ONLY protocol messages; all diagnostics go to stderr.
package mcpserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/autonoco/buttons/internal/button"
)

const (
	// defaultProtocolVersion is what we advertise if the client doesn't pin one.
	defaultProtocolVersion = "2024-11-05"
	// MaxTimeoutSeconds is the hard ceiling on any MCP-triggered press.
	MaxTimeoutSeconds = 120
	// defaultRateLimitPerMin caps calls to a single button per rolling minute.
	defaultRateLimitPerMin = 10
)

// Config tunes the server.
type Config struct {
	AllowCreate     bool   // expose buttons_create (off by default per security review)
	RateLimitPerMin int    // 0 → default 10
	Version         string // reported in serverInfo
}

// Server is a stdio MCP server. One per `buttons mcp` process.
type Server struct {
	cfg     Config
	svc     *button.Service
	mu      sync.Mutex
	calls   map[string][]time.Time // button → recent call times (rate limit window)
	running map[string]bool        // button → currently executing (1-concurrent guard)
	writeMu sync.Mutex             // serializes stdout writes across handler goroutines
}

// New builds a Server.
func New(cfg Config) *Server {
	if cfg.RateLimitPerMin <= 0 {
		cfg.RateLimitPerMin = defaultRateLimitPerMin
	}
	if cfg.Version == "" {
		cfg.Version = "dev"
	}
	return &Server{
		cfg:     cfg,
		svc:     button.NewService(),
		calls:   map[string][]time.Time{},
		running: map[string]bool{},
	}
}

func logf(format string, a ...any) { fmt.Fprintf(os.Stderr, "[mcp] "+format+"\n", a...) }

// --- JSON-RPC 2.0 wire types -------------------------------------------------

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// JSON-RPC standard error codes we use.
const (
	codeParseError    = -32700
	codeInvalidReq    = -32600
	codeMethodNotFnd  = -32601
	codeInvalidParams = -32602
	codeInternal      = -32603
)

// Serve reads JSON-RPC messages from in and writes responses to out until EOF
// or ctx cancellation. Each request is handled in its own goroutine so a long
// press doesn't block list/inspect; stdout writes are serialized.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	r := bufio.NewReaderSize(in, 1<<20)
	var wg sync.WaitGroup
	defer wg.Wait()

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		line, err := r.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var req rpcRequest
			if jerr := json.Unmarshal(bytes.TrimSpace(line), &req); jerr != nil {
				s.send(out, &rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: codeParseError, Message: "parse error"}})
			} else if isNotification(&req) {
				// Notifications (no id) get no response. We just observe them.
				s.handleNotification(&req)
			} else {
				wg.Add(1)
				go func(req rpcRequest) {
					defer wg.Done()
					resp := s.handle(ctx, &req)
					s.send(out, resp)
				}(req)
			}
		}

		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func isNotification(req *rpcRequest) bool {
	return len(req.ID) == 0 || string(req.ID) == "null"
}

func (s *Server) send(out io.Writer, resp *rpcResponse) {
	resp.JSONRPC = "2.0"
	data, err := json.Marshal(resp)
	if err != nil {
		logf("marshal response: %v", err)
		return
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, _ = out.Write(data)
	_, _ = out.Write([]byte("\n"))
}

func (s *Server) handleNotification(req *rpcRequest) {
	// initialized / cancelled / progress — nothing to do for our surface.
	logf("notification: %s", req.Method)
}

func (s *Server) handle(ctx context.Context, req *rpcRequest) *rpcResponse {
	switch req.Method {
	case "initialize":
		return s.ok(req, s.initializeResult(req))
	case "ping":
		return s.ok(req, map[string]any{})
	case "tools/list":
		return s.ok(req, map[string]any{"tools": s.toolDefs()})
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	default:
		return s.fail(req, codeMethodNotFnd, "method not found: "+req.Method)
	}
}

func (s *Server) initializeResult(req *rpcRequest) map[string]any {
	// Echo the client's requested protocol version when present; else default.
	protocol := defaultProtocolVersion
	if len(req.Params) > 0 {
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if json.Unmarshal(req.Params, &p) == nil && p.ProtocolVersion != "" {
			protocol = p.ProtocolVersion
		}
	}
	return map[string]any{
		"protocolVersion": protocol,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo":      map[string]any{"name": "buttons", "version": s.cfg.Version},
	}
}

func (s *Server) ok(req *rpcRequest, result any) *rpcResponse {
	return &rpcResponse{ID: req.ID, Result: result}
}

func (s *Server) fail(req *rpcRequest, code int, message string) *rpcResponse {
	return &rpcResponse{ID: req.ID, Error: &rpcError{Code: code, Message: message}}
}

// --- rate limit + concurrency guard -----------------------------------------

// checkRate records a call against the rolling-minute window, returning false
// if the button is over its per-minute budget.
func (s *Server) checkRate(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-time.Minute)
	kept := s.calls[name][:0]
	for _, t := range s.calls[name] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= s.cfg.RateLimitPerMin {
		s.calls[name] = kept
		return false
	}
	s.calls[name] = append(kept, now)
	return true
}

// acquire marks a button as running; returns false if it already is.
func (s *Server) acquire(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running[name] {
		return false
	}
	s.running[name] = true
	return true
}

func (s *Server) release(name string) {
	s.mu.Lock()
	delete(s.running, name)
	s.mu.Unlock()
}
