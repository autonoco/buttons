// Package deadletter persists final-failed runs so an agent can
// triage them later. Writes one JSON file per failed run to
// ~/.buttons/dead_letter/. `buttons dlq list|replay` reads from
// here.
//
// "Final" = after any retry policy has been exhausted. The engine
// writes to the DLQ from the last-hop failure path; it doesn't
// route every interim failure through here, which would flood the
// dir with noise.
package deadletter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/autonoco/buttons/internal/config"
)

// Entry is one DLQ record. Carries the minimum an agent needs to
// decide replay-vs-abandon: what ran, why it failed, and the
// args/inputs so a replay can use the same payload.
type Entry struct {
	ID         string         `json:"id"`
	Target     string         `json:"target"` // "button/<name>" or "drawer/<name>"
	FailedAt   time.Time      `json:"failed_at"`
	Code       string         `json:"code"`
	Message    string         `json:"message,omitempty"`
	FailedStep string         `json:"failed_step,omitempty"`
	Inputs     map[string]any `json:"inputs,omitempty"`
	// Raw is the full run record (history.Run for buttons,
	// drawer.Run for drawers) so replay has everything.
	Raw json.RawMessage `json:"raw,omitempty"`
}

// Record writes an entry to the DLQ. Safe to call from any press
// path; errors are returned but typically swallowed by callers so
// a DLQ write failure doesn't mask the original failure.
func Record(e Entry) error {
	dir, err := config.DeadLetterDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir dead_letter dir: %w", err)
	}
	if e.ID == "" {
		e.ID = e.FailedAt.UTC().Format("2006-01-02T15-04-05.000000")
	}
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, e.ID+".json")
	return os.WriteFile(path, data, 0o600)
}

// List returns entries newest-first up to limit (0 = all).
func List(limit int) ([]Entry, error) {
	dir, err := config.DeadLetterDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Entry{}, nil
		}
		return nil, err
	}
	out := []Entry{}
	for _, de := range entries {
		if de.IsDir() || filepath.Ext(de.Name()) != ".json" {
			continue
		}
		// #nosec G304 -- path rooted in DeadLetterDir, name from
		// os.ReadDir.
		data, err := os.ReadFile(filepath.Join(dir, de.Name()))
		if err != nil {
			continue
		}
		var e Entry
		if err := json.Unmarshal(data, &e); err != nil {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FailedAt.After(out[j].FailedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Get fetches a single entry by ID.
func Get(id string) (*Entry, error) {
	dir, err := config.DeadLetterDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, id+".json")
	// #nosec G304 -- id is either from List() output or user-supplied
	// for replay; we join with a known-safe dir and add a .json suffix.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var e Entry
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// Remove deletes a DLQ entry. Called after a successful replay so
// the same failure doesn't linger in the triage list.
func Remove(id string) error {
	dir, err := config.DeadLetterDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, id+".json")
	err = os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
