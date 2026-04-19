package drawer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
	"unicode/utf8"
)

// Run is the persisted trace of a single drawer press. One file per
// run under ~/.buttons/drawers/<name>/pressed/<ts>.json. Mirrors the
// button history Run shape: truncated output, capped history.
type Run struct {
	DrawerName string    `json:"drawer_name"`
	RunID      string    `json:"run_id"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	DurationMs int64     `json:"duration_ms"`
	Status     string    `json:"status"` // "ok" | "failed" | "cancelled"

	// Inputs is the resolved drawer input map with secret-flagged
	// entries already redacted. The executor redacts before calling
	// RecordRun, so the history file never holds plaintext secrets.
	Inputs map[string]any `json:"inputs,omitempty"`

	Steps []StepRun `json:"steps"`

	// ErrorType is populated when Status != "ok" so agents can route
	// on failure class without parsing individual step errors.
	ErrorType string `json:"error_type,omitempty"`
}

// StepRun is one step's contribution to a drawer run. Output holds
// the parsed JSON from the button's stdout when it emitted valid
// JSON; otherwise it's the raw stdout string. Args carries the
// RESOLVED args so an agent reading history can reconstruct exactly
// what ran without re-running the resolver.
type StepRun struct {
	ID         string         `json:"id"`
	Button     string         `json:"button,omitempty"`
	Status     string         `json:"status"`
	ExitCode   int            `json:"exit_code"`
	DurationMs int64          `json:"duration_ms"`
	Args       map[string]any `json:"args,omitempty"`
	Output     any            `json:"output,omitempty"`
	Stdout     string         `json:"stdout,omitempty"`
	Stderr     string         `json:"stderr,omitempty"`
	Error      *StepError     `json:"error,omitempty"`
}

// StepError is the structured error envelope surfaced on failure.
// Same shape used at the CLI boundary — remediation is the
// game-changer field for agent recovery loops.
type StepError struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	Remediation string `json:"remediation,omitempty"`
}

const (
	maxDrawerOutputBytes = 1 << 20 // 1 MB per step
	maxDrawerRunsKept    = 100
)

// RecordRun persists a completed run to the drawer's pressed/ dir
// and prunes older runs beyond the cap. The executor is expected to
// have redacted secret inputs before calling this.
func RecordRun(run Run) error {
	svc := NewService()
	pressedDir, err := svc.PressedDir(run.DrawerName)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(pressedDir, 0700); err != nil {
		return fmt.Errorf("failed to create drawer pressed directory: %w", err)
	}

	for i := range run.Steps {
		run.Steps[i].Stdout = truncateForHistory(run.Steps[i].Stdout)
		run.Steps[i].Stderr = truncateForHistory(run.Steps[i].Stderr)
	}

	data, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal drawer run: %w", err)
	}

	filename := run.StartedAt.UTC().Format("2006-01-02T15-04-05") + ".json"
	path := filepath.Join(pressedDir, filename)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write drawer run file: %w", err)
	}

	_ = pruneDrawerRuns(pressedDir, maxDrawerRunsKept)
	return nil
}

// ListRuns returns runs for one drawer, newest first.
func ListRuns(drawerName string, limit int) ([]Run, error) {
	svc := NewService()
	pressedDir, err := svc.PressedDir(drawerName)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(pressedDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Run{}, nil
		}
		return nil, fmt.Errorf("failed to read drawer pressed directory: %w", err)
	}

	runs := []Run{}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		// #nosec G304 -- pressedDir produced by Service.PressedDir which
		// rejects paths outside DrawersDir; e.Name() from os.ReadDir.
		data, err := os.ReadFile(filepath.Join(pressedDir, e.Name()))
		if err != nil {
			continue
		}
		var r Run
		if err := json.Unmarshal(data, &r); err != nil {
			continue
		}
		runs = append(runs, r)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].StartedAt.After(runs[j].StartedAt) })
	if limit > 0 && len(runs) > limit {
		runs = runs[:limit]
	}
	return runs, nil
}

// RedactSecretInputs returns a copy of values with secret-flagged
// entries replaced by "[redacted]". Called by the executor before
// RecordRun; never expose this to CLI output paths — pressing a
// drawer with secrets should still echo them into the button's env,
// just not into the on-disk trace.
func RedactSecretInputs(defs []InputDef, values map[string]any) map[string]any {
	if len(values) == 0 {
		return values
	}
	secret := map[string]bool{}
	for _, d := range defs {
		if d.Secret {
			secret[d.Name] = true
		}
	}
	out := make(map[string]any, len(values))
	for k, v := range values {
		if secret[k] {
			out[k] = "[redacted]"
		} else {
			out[k] = v
		}
	}
	return out
}

func pruneDrawerRuns(pressedDir string, keep int) error {
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
	sort.Strings(names)
	for _, name := range names[:len(names)-keep] {
		_ = os.Remove(filepath.Join(pressedDir, name))
	}
	return nil
}

func truncateForHistory(s string) string {
	if len(s) <= maxDrawerOutputBytes {
		return s
	}
	cut := maxDrawerOutputBytes
	for cut > maxDrawerOutputBytes-4 && cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "\n[truncated]"
}
