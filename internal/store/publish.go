package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

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
// of InstallSpec: gather the button folder (button.json + main.* + AGENT.md,
// never pressed/ history), content-hash it, and publish.
func Publish(dst Publisher, name string) (*PublishResult, error) {
	dir, err := config.ButtonDir(button.Slugify(name))
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("button %q not found: %w", name, err)
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
		return nil, fmt.Errorf("button %q has no button.json", name)
	}
	var spec button.Button
	if err := json.Unmarshal(specData, &spec); err != nil {
		return nil, fmt.Errorf("button %q: invalid button.json: %w", name, err)
	}

	bundle := &Bundle{Name: spec.Name, Version: spec.Version, SHA256: hashFiles(files), Files: files}
	if err := dst.Publish(bundle); err != nil {
		return nil, err
	}
	return &PublishResult{Name: bundle.Name, Version: bundle.Version, SHA256: bundle.SHA256, Files: len(files)}, nil
}

// Publish writes a bundle into the LocalSource directory as <Root>/<name>/…,
// making it installable by `buttons install <name> --source <Root>`. Immutable
// in spirit: a re-publish overwrites the folder (the registry enforces true
// version immutability; a dir source is dev-grade).
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
