package button

import "time"

type Button struct {
	SchemaVersion        int               `json:"schema_version"`
	Name                 string            `json:"name"`
	Description          string            `json:"description,omitempty"`
	Runtime              string            `json:"runtime"`
	URL                  string            `json:"url,omitempty"`
	Method               string            `json:"method,omitempty"`
	Headers              map[string]string `json:"headers,omitempty"`
	Body                 string            `json:"body,omitempty"`
	Args                 []ArgDef          `json:"args,omitempty"`
	Env                  map[string]string `json:"env"`
	TimeoutSeconds       int               `json:"timeout_seconds"`
	MaxResponseBytes     int64             `json:"max_response_bytes,omitempty"`
	AllowPrivateNetworks bool              `json:"allow_private_networks,omitempty"`
	MCPEnabled           bool              `json:"mcp_enabled"`
	// Pinned buttons render as large, clickable cards at the top of the
	// `buttons board` TUI. Omitted from JSON when false to avoid polluting
	// every existing button.json — most buttons aren't pinned.
	Pinned    bool      `json:"pinned,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ArgDef struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

// ExtForRuntime returns the code file extension for a given runtime.
func ExtForRuntime(runtime string) string {
	switch runtime {
	case "python", "python3":
		return ".py"
	case "node", "javascript", "js":
		return ".js"
	default:
		return ".sh"
	}
}

// scaffoldFor returns the placeholder body written to main.<ext> when a
// button is created without --code or --file. The shebang makes the runtime
// obvious when the agent opens the file; the TODO comment signals where to
// put the real implementation.
func scaffoldFor(runtime string) string {
	switch runtime {
	case "python", "python3":
		return "#!/usr/bin/env python3\n# TODO: add your code here\n# Args arrive as os.environ[\"BUTTONS_ARG_<NAME>\"]\n"
	case "node", "javascript", "js":
		return "#!/usr/bin/env node\n// TODO: add your code here\n// Args arrive as process.env.BUTTONS_ARG_<NAME>\n"
	default:
		return "#!/bin/sh\nset -eu\n\n# TODO: add your command here\n# Args arrive as $BUTTONS_ARG_<NAME>\n"
	}
}
