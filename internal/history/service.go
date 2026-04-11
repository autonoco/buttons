package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/engine"
)

const maxOutputBytes = 1 << 20 // 1MB

// Run represents a persisted execution record.
type Run struct {
	ButtonName     string            `json:"button_name"`
	StartedAt      time.Time         `json:"started_at"`
	FinishedAt     time.Time         `json:"finished_at"`
	ExitCode       int               `json:"exit_code"`
	HTTPStatusCode int               `json:"http_status_code,omitempty"`
	Status         string            `json:"status"`
	ErrorType      string            `json:"error_type,omitempty"`
	Stdout         string            `json:"stdout"`
	Stderr         string            `json:"stderr"`
	DurationMs     int64             `json:"duration_ms"`
	Args           map[string]string `json:"args,omitempty"`
}

// Record writes an execution result as a JSON file in the button's pressed/ directory.
func Record(result *engine.Result) error {
	svc := button.NewService()
	pressedDir, err := svc.PressedDir(result.Button)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(pressedDir, 0700); err != nil {
		return fmt.Errorf("failed to create pressed directory: %w", err)
	}

	finishedAt := result.StartedAt.Add(time.Duration(result.DurationMs) * time.Millisecond)

	run := Run{
		ButtonName:     result.Button,
		StartedAt:      result.StartedAt.UTC(),
		FinishedAt:     finishedAt.UTC(),
		ExitCode:       result.ExitCode,
		HTTPStatusCode: result.HTTPStatusCode,
		Status:         result.Status,
		ErrorType:      result.ErrorType,
		Stdout:         truncate(result.Stdout),
		Stderr:         truncate(result.Stderr),
		DurationMs:     result.DurationMs,
		Args:           result.Args,
	}

	data, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal run: %w", err)
	}

	filename := result.StartedAt.UTC().Format("2006-01-02T15-04-05") + ".json"
	runPath := filepath.Join(pressedDir, filename)

	if err := os.WriteFile(runPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write run file: %w", err)
	}

	return nil
}

// List returns runs for a button, ordered by most recent first.
func List(buttonName string, limit int) ([]Run, error) {
	svc := button.NewService()
	pressedDir, err := svc.PressedDir(buttonName)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(pressedDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Run{}, nil
		}
		return nil, fmt.Errorf("failed to read pressed directory: %w", err)
	}

	runs := []Run{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		// #nosec G304 -- pressedDir is produced by button.Service.PressedDir()
		// which rejects paths outside ButtonsDir; entry.Name() comes from the
		// os.ReadDir listing of that same directory, not user input.
		data, err := os.ReadFile(filepath.Join(pressedDir, entry.Name()))
		if err != nil {
			continue
		}
		var run Run
		if err := json.Unmarshal(data, &run); err != nil {
			continue
		}
		runs = append(runs, run)
	}

	// Sort by started_at descending (most recent first)
	sort.Slice(runs, func(i, j int) bool {
		return runs[i].StartedAt.After(runs[j].StartedAt)
	})

	if limit > 0 && len(runs) > limit {
		runs = runs[:limit]
	}

	return runs, nil
}

// ListAll returns runs across all buttons, ordered by most recent first.
func ListAll(limit int) ([]Run, error) {
	svc := button.NewService()
	buttons, err := svc.List()
	if err != nil {
		return nil, err
	}

	allRuns := []Run{}
	for _, btn := range buttons {
		runs, err := List(btn.Name, 0) // get all, we'll limit after merge
		if err != nil {
			continue
		}
		allRuns = append(allRuns, runs...)
	}

	sort.Slice(allRuns, func(i, j int) bool {
		return allRuns[i].StartedAt.After(allRuns[j].StartedAt)
	})

	if limit > 0 && len(allRuns) > limit {
		allRuns = allRuns[:limit]
	}

	return allRuns, nil
}

func truncate(s string) string {
	if len(s) <= maxOutputBytes {
		return s
	}
	cut := maxOutputBytes
	for cut > maxOutputBytes-4 && cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "\n[truncated]"
}
