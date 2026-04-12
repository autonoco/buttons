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
}
