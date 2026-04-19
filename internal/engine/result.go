package engine

import "time"

type Result struct {
	Status         string            `json:"status"`
	ExitCode       int               `json:"exit_code"`
	HTTPStatusCode int               `json:"http_status_code,omitempty"`
	Stdout         string            `json:"stdout"`
	Stderr         string            `json:"stderr"`
	DurationMs     int64             `json:"duration_ms"`
	ErrorType      string            `json:"error_type,omitempty"`
	Prompt         string            `json:"prompt,omitempty"`
	Button         string            `json:"button"`
	Args           map[string]string `json:"args,omitempty"`
	StartedAt      time.Time         `json:"started_at"`
	// ProgressPath is the JSONL file scripts can append structured
	// events to (via $BUTTONS_PROGRESS_PATH). Empty when progress
	// streaming isn't set up (e.g., HTTP/prompt buttons). `buttons
	// tail` follows this path to surface live progress for humans
	// and agents.
	ProgressPath string `json:"progress_path,omitempty"`
}
