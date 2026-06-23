package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/config"
)

// UpdateStatus is one installed button's outcome in an update run.
type UpdateStatus struct {
	Name   string `json:"name"`
	Action string `json:"action"` // updated | available | unchanged | skipped | error
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// UpdateResult is the full content-update report.
type UpdateResult struct {
	Buttons []UpdateStatus `json:"buttons"`
}

// Counts returns a {action: n} summary for human/JSON output.
func (r *UpdateResult) Counts() map[string]int {
	out := map[string]int{}
	for _, b := range r.Buttons {
		out[b.Action]++
	}
	return out
}

// SourceResolver reconstructs a Source from an installed button's stamped
// `source` ref so update knows where to re-fetch from. Injectable for tests.
type SourceResolver func(ref string) (Source, error)

// DefaultSourceResolver understands the source refs install stamps today.
// Only "local:<dir>" is wired; registry refs ("https://…", "registry:…")
// land with the HTTPSource in #275/#276.
func DefaultSourceResolver(ref string) (Source, error) {
	if dir, ok := strings.CutPrefix(ref, "local:"); ok {
		return &LocalSource{Root: dir}, nil
	}
	return nil, fmt.Errorf("unsupported source %q (only local: is wired today; the registry source lands in #275)", ref)
}

// UpdateInstalled reconciles every installed button against its source. When
// check is true it reports what would change without writing (action
// "available"); otherwise it re-installs drifted buttons (action "updated").
//
// Buttons with no source (hand-authored) or an unresolvable source are
// "skipped", never an error — one un-sourced or registry-pinned button can't
// fail the whole run. A source that resolves but fails to fetch is an "error"
// for that button alone.
//
// Drift is detected by content hash: the installed button.json records the
// SHA256 of its source files at install time (#190), and Fetch recomputes that
// same hash from the current source — equal means unchanged.
func UpdateInstalled(resolve SourceResolver, check bool) (*UpdateResult, error) {
	if resolve == nil {
		resolve = DefaultSourceResolver
	}
	installed, err := listInstalled()
	if err != nil {
		return nil, err
	}
	res := &UpdateResult{}
	for _, b := range installed {
		res.Buttons = append(res.Buttons, reconcile(b, resolve, check))
	}
	sort.Slice(res.Buttons, func(i, j int) bool { return res.Buttons[i].Name < res.Buttons[j].Name })
	return res, nil
}

func reconcile(b button.Button, resolve SourceResolver, check bool) UpdateStatus {
	st := UpdateStatus{Name: b.Name, From: b.Version}
	if b.Source == "" {
		st.Action = "skipped"
		st.Reason = "no source (locally created)"
		return st
	}
	src, err := resolve(b.Source)
	if err != nil {
		st.Action = "skipped"
		st.Reason = err.Error()
		return st
	}
	bundle, err := src.Fetch(b.Name, "")
	if err != nil {
		st.Action = "error"
		st.Reason = err.Error()
		return st
	}
	if bundle.SHA256 == b.ContentHash {
		st.Action = "unchanged"
		return st
	}
	st.To = bundle.Version
	if check {
		st.Action = "available"
		return st
	}
	// Re-install re-fetches, re-verifies the hash, re-stamps, and writes the
	// button folder from the same source ref it was installed from.
	if _, err := install(src, b.Name, "", b.Source); err != nil {
		st.Action = "error"
		st.Reason = err.Error()
		return st
	}
	st.Action = "updated"
	return st
}

// listInstalled reads every installed button.json from the buttons dir.
// Unparseable folders are skipped (consistent with button.Service.List).
func listInstalled() ([]button.Button, error) {
	dir, err := config.ButtonsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []button.Button
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// #nosec G304 -- rooted in ButtonsDir + an enumerated DirEntry name.
		data, err := os.ReadFile(filepath.Join(dir, e.Name(), "button.json"))
		if err != nil {
			continue
		}
		var b button.Button
		if json.Unmarshal(data, &b) != nil {
			continue
		}
		out = append(out, b)
	}
	return out, nil
}
