// Package store installs and updates buttons from a Source — the registry
// (#275) over HTTP, or a local directory for dev/testing. The CLI codes
// against the Source interface so the backend is swappable.
package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/autonoco/buttons/internal/button"
)

// ButtonRef is a catalog entry — what a Source advertises in its index.
// Used for search and tag-collection resolution.
type ButtonRef struct {
	Name    string   `json:"name"`
	Version string   `json:"version,omitempty"`
	Tags    []string `json:"tags,omitempty"`
	SHA256  string   `json:"sha256,omitempty"`
}

// Bundle is a fetched button's content, ready to write to disk.
type Bundle struct {
	Name    string
	Version string
	SHA256  string            // content hash of Files (provenance pin)
	Files   map[string][]byte // relative filename -> bytes (button.json, main.*, AGENT.md)
}

// Source is where buttons are installed/updated from. HTTPSource (the registry,
// #275) and LocalSource both satisfy it; the CLI never hard-codes one.
type Source interface {
	// Index returns the catalog (for `search` + resolving `install tag:<x>`).
	Index() ([]ButtonRef, error)
	// Fetch returns a button's content. version "" means latest.
	Fetch(name, version string) (*Bundle, error)
}

// hashFiles returns a deterministic SHA256 over a set of files (sorted by name).
func hashFiles(files map[string][]byte) string {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		fmt.Fprintf(h, "%s\x00%d\x00", n, len(files[n]))
		h.Write(files[n])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// LocalSource installs from a directory laid out like a buttons dir:
// <Root>/<name>/{button.json, main.*, AGENT.md}. For dev/testing without a
// registry server, and the reference for what an HTTPSource must reproduce.
type LocalSource struct {
	Root string
}

func (s *LocalSource) Index() ([]ButtonRef, error) {
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		return nil, fmt.Errorf("source %q: %w", s.Root, err)
	}
	var refs []ButtonRef
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// #nosec G304 -- path is rooted in s.Root + a DirEntry name we just enumerated.
		data, err := os.ReadFile(filepath.Join(s.Root, e.Name(), "button.json"))
		if err != nil {
			continue // not a button folder
		}
		var b button.Button
		if json.Unmarshal(data, &b) != nil {
			continue
		}
		refs = append(refs, ButtonRef{Name: b.Name, Version: b.Version, Tags: b.Tags})
	}
	return refs, nil
}

func (s *LocalSource) Fetch(name, _ string) (*Bundle, error) {
	dir := filepath.Join(s.Root, name)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("button %q not found in source: %w", name, err)
	}
	files := map[string][]byte{}
	for _, e := range entries {
		if e.IsDir() {
			continue // skip pressed/
		}
		// #nosec G304 -- dir/name both come from enumerated entries under s.Root.
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		files[e.Name()] = data
	}
	if _, ok := files["button.json"]; !ok {
		return nil, fmt.Errorf("button %q has no button.json", name)
	}
	var b button.Button
	if err := json.Unmarshal(files["button.json"], &b); err != nil {
		return nil, fmt.Errorf("button %q: invalid button.json: %w", name, err)
	}
	return &Bundle{Name: b.Name, Version: b.Version, SHA256: hashFiles(files), Files: files}, nil
}
