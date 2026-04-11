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
	CreatedAt            time.Time         `json:"created_at"`
	UpdatedAt            time.Time         `json:"updated_at"`
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
