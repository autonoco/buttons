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

// Publisher accepts a button Bundle for distribution — the write-side mirror of
// Source.Fetch. LocalSource implements it (publish to a directory); the
// registry HTTPSource implements it over `POST /v1/buttons` (#276/#275).
type Publisher interface {
	Publish(b *Bundle) error
}

// PublishResult reports what a publish produced.
type PublishResult struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	SHA256  string `json:"sha256"`
	Files   int    `json:"files"`
}

// Publish reads an installed/local button and hands it to dst. It's the inverse
// of InstallSpec: gather the button folder (button.json + main.* + AGENTS.md,
// never pressed/ history), content-hash it, and publish under the button's own
// name (the LocalSource path).
func Publish(dst Publisher, name string) (*PublishResult, error) {
	bundle, err := loadLocalBundle(name, "")
	if err != nil {
		return nil, err
	}
	if err := dst.Publish(bundle); err != nil {
		return nil, err
	}
	return &PublishResult{Name: bundle.Name, Version: bundle.Version, SHA256: bundle.SHA256, Files: len(bundle.Files)}, nil
}

// PublishToRegistry publishes a local button to the hosted registry under a
// scoped identity (@desk/name). The on-disk button is found by its bare name;
// the @desk scope is registry identity, assigned here (not stored on disk). A
// version is required — the registry pins immutable versions.
func PublishToRegistry(dst Publisher, ref string) (*PublishResult, error) {
	_, localName, err := splitScoped(ref)
	if err != nil {
		return nil, err
	}
	bundle, err := loadLocalBundle(localName, ref)
	if err != nil {
		return nil, err
	}
	if bundle.Version == "" {
		return nil, fmt.Errorf("registry publish requires a version: set \"version\" in %q's button.json", localName)
	}
	if err := dst.Publish(bundle); err != nil {
		return nil, err
	}
	return &PublishResult{Name: bundle.Name, Version: bundle.Version, SHA256: bundle.SHA256, Files: len(bundle.Files)}, nil
}

// loadLocalBundle reads a local button folder into a Bundle. localName locates
// the on-disk folder (slugified); identity, when non-empty, overrides the
// stamped Bundle.Name with the registry id (@desk/name) — for a LocalSource it
// stays empty so the button keeps its own name.
func loadLocalBundle(localName, identity string) (*Bundle, error) {
	dir, err := config.ButtonDir(button.Slugify(localName))
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("button %q not found: %w", localName, err)
	}

	files := map[string][]byte{}
	for _, e := range entries {
		if e.IsDir() {
			continue // skip pressed/ — run history isn't part of the artifact
		}
		// #nosec G304 -- dir is config.ButtonDir (rooted, slugified) + an enumerated entry.
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		files[e.Name()] = data
	}
	specData, ok := files["button.json"]
	if !ok {
		return nil, fmt.Errorf("button %q has no button.json", localName)
	}
	var spec button.Button
	if err := json.Unmarshal(specData, &spec); err != nil {
		return nil, fmt.Errorf("button %q: invalid button.json: %w", localName, err)
	}

	name := spec.Name
	if identity != "" {
		name = identity
	}
	return &Bundle{Name: name, Version: spec.Version, SHA256: hashFiles(files), Files: files}, nil
}

// splitScoped parses a registry identity @desk/name into its parts, rejecting
// anything that isn't a single-segment name under an @scope.
func splitScoped(ref string) (desk, name string, err error) {
	bad := fmt.Errorf("registry name must be scoped like @desk/name, got %q", ref)
	if !strings.HasPrefix(ref, "@") {
		return "", "", bad
	}
	i := strings.IndexByte(ref, '/')
	if i < 0 || i == len(ref)-1 || i == 1 { // need chars after @ and after /
		return "", "", bad
	}
	desk, name = ref[:i], ref[i+1:]
	if strings.ContainsRune(name, '/') {
		return "", "", bad
	}
	return desk, name, nil
}

// Publish writes a bundle into the LocalSource directory as <Root>/<name>/…,
// making it fetchable by LocalSource in tests/dev helpers. Immutable in spirit:
// a re-publish overwrites the folder (the registry enforces true version
// immutability; a dir source is dev-grade).
func (s *LocalSource) Publish(b *Bundle) error {
	dir := filepath.Join(s.Root, b.Name)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create publish dir: %w", err)
	}
	for rel, data := range b.Files {
		// #nosec G306 -- published button files are user-owned content in a user dir.
		if err := os.WriteFile(filepath.Join(dir, rel), data, 0600); err != nil {
			return fmt.Errorf("write %s: %w", rel, err)
		}
	}
	return nil
}
