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

// maxRunsPerButton caps how many press records we keep on disk per
// button. After each Record, older entries beyond this cap are deleted
// so `pressed/` doesn't grow unbounded for long-running automations.
// 100 is generous for debugging ("what did the last few presses do?")
// while still bounded — at ~1 MB per run that's 100 MB worst case per
// button, and typical payloads are much smaller.
const maxRunsPerButton = 100

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

	// Trim old runs so the pressed/ directory stays bounded. Swallow
	// prune errors — a failed prune shouldn't fail the press path the
	// user actually cares about; worst case the directory grows a
	// little and the next successful Record catches up.
	_ = pruneOldRuns(pressedDir, maxRunsPerButton)

	return nil
}

// pruneOldRuns keeps the N most recent *.json files in pressedDir and
// deletes the rest. Ordering uses the filename (which is an ISO-ish
// UTC timestamp set by Record) so we don't have to stat every file —
// cheaper and monotonic with how files are named.
func pruneOldRuns(pressedDir string, keep int) error {
	if keep <= 0 {
		return nil
	}
	entries, err := os.ReadDir(pressedDir)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		names = append(names, e.Name())
	}
	if len(names) <= keep {
		return nil
	}
	// Sort ascending (oldest first) so the slice to delete is the head.
	sort.Strings(names)
	toDelete := names[:len(names)-keep]
	for _, name := range toDelete {
		// Best-effort; a single failed unlink shouldn't stop the rest.
		_ = os.Remove(filepath.Join(pressedDir, name))
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
