package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/history"
	"github.com/autonoco/buttons/internal/runner"
)

// toolDefs is the meta-tool surface advertised via tools/list. Thin on purpose
// (3, +1 optional) so MCP clients don't degrade under a tool-per-button blowup.
func (s *Server) toolDefs() []map[string]any {
	defs := []map[string]any{
		{
			"name":        "buttons_list",
			"description": "List buttons exposed to MCP (mcp_enabled: true), with their declared args.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			"name":        "buttons_press",
			"description": "Execute a button by name. Args are validated against the button's spec and passed to the script as BUTTONS_ARG_<NAME> environment variables (never substituted into shell text). Timeout is capped at 120s.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":    map[string]any{"type": "string", "description": "button name"},
					"args":    map[string]any{"type": "object", "description": "key→value string arguments", "additionalProperties": map[string]any{"type": "string"}},
					"timeout": map[string]any{"type": "integer", "description": "override timeout in seconds (hard-capped at 120)"},
				},
				"required": []string{"name"},
			},
		},
		{
			"name":        "buttons_inspect",
			"description": "Get a button's full spec plus its last 5 runs.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
				"required":   []string{"name"},
			},
		},
	}
	if s.cfg.AllowCreate {
		defs = append(defs, map[string]any{
			"name":        "buttons_create",
			"description": "Create a new shell/code button. Available only because the server was started with --allow-create.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":        map[string]any{"type": "string"},
					"runtime":     map[string]any{"type": "string", "enum": []string{"shell", "bash", "python", "node"}},
					"code":        map[string]any{"type": "string"},
					"description": map[string]any{"type": "string"},
				},
				"required": []string{"name", "code"},
			},
		})
	}
	return defs
}

func (s *Server) handleToolsCall(ctx context.Context, req *rpcRequest) *rpcResponse {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return s.fail(req, codeInvalidParams, "invalid params: "+err.Error())
		}
	}

	var result map[string]any
	switch p.Name {
	case "buttons_list":
		result = s.toolList(p.Arguments)
	case "buttons_press":
		result = s.toolPress(ctx, p.Arguments)
	case "buttons_inspect":
		result = s.toolInspect(p.Arguments)
	case "buttons_create":
		if !s.cfg.AllowCreate {
			result = toolError("FORBIDDEN", "buttons_create is disabled (start `buttons mcp --allow-create`)", "", 0)
		} else {
			result = s.toolCreate(p.Arguments)
		}
	default:
		return s.fail(req, codeMethodNotFnd, "unknown tool: "+p.Name)
	}
	return s.ok(req, result)
}

func (s *Server) toolList(_ json.RawMessage) map[string]any {
	buttons, err := s.svc.List()
	if err != nil {
		return toolError("INTERNAL", err.Error(), "", 0)
	}
	type entry struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Args        []button.ArgDef `json:"args,omitempty"`
		Tags        []string        `json:"tags,omitempty"`
	}
	out := []entry{}
	for _, b := range buttons {
		if !b.MCPEnabled {
			continue
		}
		out = append(out, entry{Name: b.Name, Description: b.Description, Args: b.Args, Tags: b.Tags})
	}
	return toolJSON(map[string]any{"buttons": out, "count": len(out)})
}

func (s *Server) toolPress(ctx context.Context, raw json.RawMessage) map[string]any {
	var p struct {
		Name    string            `json:"name"`
		Args    map[string]string `json:"args"`
		Timeout int               `json:"timeout"`
	}
	if err := json.Unmarshal(nonEmpty(raw), &p); err != nil {
		return toolError("INVALID_PARAMS", "invalid arguments: "+err.Error(), "", 0)
	}
	if p.Name == "" {
		return toolError("INVALID_PARAMS", "missing required field: name", "", 0)
	}

	btn, err := s.svc.Get(p.Name)
	if err != nil {
		return toolError("NOT_FOUND", "button not found: "+p.Name, p.Name, 0)
	}
	if !btn.MCPEnabled {
		return toolError("FORBIDDEN", "button not exposed to MCP (set mcp_enabled: true)", p.Name, 0)
	}

	// Rate limit, then single-concurrency guard — both per button.
	if !s.checkRate(p.Name) {
		return toolError("RATE_LIMITED", fmt.Sprintf("rate limit: max %d calls/min to %q", s.cfg.RateLimitPerMin, p.Name), p.Name, 0)
	}
	if !s.acquire(p.Name) {
		return toolError("BUSY", "button is already executing (1 concurrent press per button)", p.Name, 0)
	}
	defer s.release(p.Name)

	start := time.Now()
	res, perr := runner.Press(ctx, p.Name, p.Args, runner.Options{
		TimeoutSeconds:    p.Timeout,
		MaxTimeoutSeconds: MaxTimeoutSeconds,
		RecordHistory:     true,
	})
	if perr != nil {
		code := "PRESS_ERROR"
		var se *button.ServiceError
		if errors.As(perr, &se) {
			code = se.Code
		}
		return toolError(code, perr.Error(), p.Name, time.Since(start).Milliseconds())
	}
	if res.Status != "ok" {
		code := res.ErrorType
		if code == "" {
			code = strings.ToUpper(res.Status)
		}
		msg := strings.TrimSpace(res.Stderr)
		if msg == "" {
			msg = "button " + res.Status
		}
		return toolError(code, msg, p.Name, res.DurationMs)
	}
	return toolJSON(map[string]any{
		"button":      res.Button,
		"status":      res.Status,
		"exit_code":   res.ExitCode,
		"stdout":      res.Stdout,
		"stderr":      res.Stderr,
		"duration_ms": res.DurationMs,
	})
}

func (s *Server) toolInspect(raw json.RawMessage) map[string]any {
	var p struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(nonEmpty(raw), &p); err != nil {
		return toolError("INVALID_PARAMS", "invalid arguments: "+err.Error(), "", 0)
	}
	if p.Name == "" {
		return toolError("INVALID_PARAMS", "missing required field: name", "", 0)
	}
	btn, err := s.svc.Get(p.Name)
	if err != nil {
		return toolError("NOT_FOUND", "button not found: "+p.Name, p.Name, 0)
	}
	if !btn.MCPEnabled {
		return toolError("FORBIDDEN", "button not exposed to MCP (set mcp_enabled: true)", p.Name, 0)
	}
	runs, _ := history.List(p.Name, 5)
	if runs == nil {
		runs = []history.Run{}
	}
	return toolJSON(map[string]any{"button": btn, "recent_runs": runs})
}

func (s *Server) toolCreate(raw json.RawMessage) map[string]any {
	var p struct {
		Name        string `json:"name"`
		Runtime     string `json:"runtime"`
		Code        string `json:"code"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(nonEmpty(raw), &p); err != nil {
		return toolError("INVALID_PARAMS", "invalid arguments: "+err.Error(), "", 0)
	}
	if p.Name == "" || p.Code == "" {
		return toolError("INVALID_PARAMS", "name and code are required", "", 0)
	}
	runtime := p.Runtime
	if runtime == "" {
		runtime = "shell"
	}
	btn, err := s.svc.Create(button.CreateOpts{
		Name:        p.Name,
		Code:        p.Code,
		Runtime:     runtime,
		Description: p.Description,
	})
	if err != nil {
		code := "CREATE_ERROR"
		var se *button.ServiceError
		if errors.As(err, &se) {
			code = se.Code
		}
		return toolError(code, err.Error(), p.Name, 0)
	}
	return toolJSON(map[string]any{"created": btn.Name, "button": btn})
}

// --- tool result helpers (MCP content blocks) --------------------------------

// toolText wraps text in the MCP tool-result content shape.
func toolText(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
	}
}

// toolJSON renders v as pretty JSON in a successful text result.
func toolJSON(v any) map[string]any {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return toolError("INTERNAL", "could not encode result: "+err.Error(), "", 0)
	}
	return toolText(string(b), false)
}

// toolError renders the review-mandated structured error shape as an error result:
// {"error":true,"code":"TIMEOUT","message":"…","button":"…","duration_ms":N}.
func toolError(code, message, btn string, durationMs int64) map[string]any {
	payload := map[string]any{"error": true, "code": code, "message": message}
	if btn != "" {
		payload["button"] = btn
	}
	if durationMs > 0 {
		payload["duration_ms"] = durationMs
	}
	b, _ := json.Marshal(payload)
	return toolText(string(b), true)
}

func nonEmpty(raw json.RawMessage) []byte {
	if len(bytes.TrimSpace(raw)) == 0 {
		return []byte("{}")
	}
	return raw
}
