package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/config"
)

// Result is what an install run produced, for structured output.
type Result struct {
	Installed []string `json:"installed"` // button names written this run
}

// InstallSpec installs a spec from src and returns the names written. The spec is:
//   - "name" or "name@version" — one button (+ its `requires` deps, transitively)
//   - "tag:<x>"                — every button in the source carrying tag <x> (+ deps)
//
// sourceRef is recorded in each installed button.json's `source` field (provenance).
func InstallSpec(src Source, spec, sourceRef string) (*Result, error) {
	seen := map[string]bool{}
	var installed []string

	var roots []string
	var version string
	if tag, ok := strings.CutPrefix(spec, "tag:"); ok {
		names, err := resolveTag(src, tag)
		if err != nil {
			return nil, err
		}
		if len(names) == 0 {
			return nil, fmt.Errorf("no buttons carry tag %q", tag)
		}
		roots = names
	} else {
		name, v := splitVersion(spec)
		roots = []string{name}
		version = v
	}

	// Install each root + its `requires` closure (deps come from the same source).
	for _, name := range roots {
		if err := installWithDeps(src, name, version, sourceRef, seen, &installed); err != nil {
			return nil, err
		}
		version = "" // version pin only applies to the explicitly-named root
	}
	return &Result{Installed: installed}, nil
}

func installWithDeps(src Source, name, version, sourceRef string, seen map[string]bool, out *[]string) error {
	key := button.Slugify(name)
	if seen[key] {
		return nil
	}
	seen[key] = true

	b, err := install(src, name, version, sourceRef)
	if err != nil {
		return err
	}
	*out = append(*out, b.Name)

	for _, dep := range b.Requires {
		if err := installWithDeps(src, dep, "", sourceRef, seen, out); err != nil {
			return fmt.Errorf("dependency of %q: %w", b.Name, err)
		}
	}
	return nil
}

// install fetches one button and writes it into the buttons data dir, stamping
// source/version/content_hash into its button.json. Returns the installed spec.
func install(src Source, name, version, sourceRef string) (*button.Button, error) {
	bundle, err := src.Fetch(name, version)
	if err != nil {
		return nil, err
	}

	var spec button.Button
	if err := json.Unmarshal(bundle.Files["button.json"], &spec); err != nil {
		return nil, fmt.Errorf("button %q: invalid button.json: %w", name, err)
	}
	spec.Source = sourceRef
	spec.Version = bundle.Version
	spec.ContentHash = bundle.SHA256
	stamped, err := json.MarshalIndent(&spec, "", "  ")
	if err != nil {
		return nil, err
	}
	bundle.Files["button.json"] = stamped

	dir, err := config.ButtonDir(button.Slugify(spec.Name))
	if err != nil {
		return nil, err
	}
	// Validate every bundle path BEFORE creating dirs or writing, so a rejected
	// (traversal) bundle leaves nothing partially installed.
	dsts := make(map[string]string, len(bundle.Files))
	for rel := range bundle.Files {
		dst, err := safeJoin(dir, rel)
		if err != nil {
			return nil, err
		}
		dsts[rel] = dst
	}
	if err := os.MkdirAll(filepath.Join(dir, "pressed"), 0700); err != nil {
		return nil, fmt.Errorf("create button dir: %w", err)
	}
	for rel, data := range bundle.Files {
		dst := dsts[rel]
		mode := os.FileMode(0600)
		if strings.HasPrefix(rel, "main.") {
			mode = 0700 // #nosec G302 -- code files need the exec bit to run via sh/python/node
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
			return nil, fmt.Errorf("create parent for %s: %w", rel, err)
		}
		if err := os.WriteFile(dst, data, mode); err != nil {
			return nil, fmt.Errorf("write %s: %w", rel, err)
		}
	}
	return &spec, nil
}

// resolveTag returns the names of every button in the source carrying tag.
func resolveTag(src Source, tag string) ([]string, error) {
	refs, err := src.Index()
	if err != nil {
		return nil, err
	}
	var names []string
	for _, r := range refs {
		for _, t := range r.Tags {
			if t == tag {
				names = append(names, r.Name)
				break
			}
		}
	}
	return names, nil
}

// splitVersion parses "name@version" → (name, version). No "@" → version "".
func splitVersion(spec string) (name, version string) {
	if i := strings.LastIndex(spec, "@"); i > 0 {
		return spec[:i], spec[i+1:]
	}
	return spec, ""
}
