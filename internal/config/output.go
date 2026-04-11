package config

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/mattn/go-isatty"
)

func IsNonTTY() bool {
	return !isatty.IsTerminal(os.Stdout.Fd()) && !isatty.IsCygwinTerminal(os.Stdout.Fd())
}

type Response struct {
	OK    bool        `json:"ok"`
	Data  any `json:"data,omitempty"`
	Error *ErrorInfo  `json:"error,omitempty"`
}

type ErrorInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
	Spec    any    `json:"spec,omitempty"`
}

func WriteJSON(data any) error {
	resp := Response{OK: true, Data: data}
	return writeResponse(resp)
}

func WriteJSONError(code string, message string) error {
	resp := Response{
		OK:    false,
		Error: &ErrorInfo{Code: code, Message: message},
	}
	return writeResponse(resp)
}

// WriteJSONErrorWithHint writes a JSON error with a recovery hint and optional spec.
func WriteJSONErrorWithHint(code, message, hint string, spec any) error {
	resp := Response{
		OK: false,
		Error: &ErrorInfo{
			Code:    code,
			Message: message,
			Hint:    hint,
			Spec:    spec,
		},
	}
	return writeResponse(resp)
}

func writeResponse(resp Response) error {
	out, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	fmt.Println(string(out))
	return nil
}
