package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/drawer"
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
	Kind    string `json:"kind"`
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
	return &PublishResult{Name: bundle.Name, Kind: bundle.Kind, Version: bundle.Version, SHA256: bundle.SHA256, Files: len(bundle.Files)}, nil
}

// PublishToRegistry publishes a local button to the hosted registry under a
// scoped identity (@desk/name). The on-disk button is found by its bare name;
// the @desk scope is registry identity, assigned here (not stored on disk). A
// publish stamps a simple integer version because the registry pins immutable
// versions and users should not have to hand-edit release counters.
func PublishToRegistry(dst Publisher, ref string) (*PublishResult, error) {
	_, localName, err := splitScoped(ref)
	if err != nil {
		return nil, err
	}
	bundle, err := loadLocalBundle(localName, ref)
	if err != nil {
		return nil, err
	}
	version := strings.TrimSpace(bundle.Version)
	if version == "" {
		version = button.InitialContentVersion
	}

	for attempts := 0; attempts < 1000; attempts++ {
		if err := stampLocalBundleVersion(localName, bundle, version); err != nil {
			return nil, err
		}
		if err := dst.Publish(bundle); err != nil {
			if !isVersionExists(err) {
				return nil, err
			}
			next, bumpErr := button.NextVersion(version)
			if bumpErr != nil {
				return nil, fmt.Errorf("registry version %q already exists and cannot be auto-bumped: %w", version, bumpErr)
			}
			version = next
			continue
		}
		return &PublishResult{Name: bundle.Name, Kind: bundle.Kind, Version: bundle.Version, SHA256: bundle.SHA256, Files: len(bundle.Files)}, nil
	}
	return nil, fmt.Errorf("registry publish could not find an unused version after 1000 attempts")
}

// loadLocalBundle reads a local button or drawer folder into a Bundle. localName
// locates the on-disk folder (slugified); identity, when non-empty, overrides
// the stamped Bundle.Name with the registry id (@desk/name) — for a LocalSource
// it stays empty so the package keeps its own local name.
func loadLocalBundle(localName, identity string) (*Bundle, error) {
	slug := button.Slugify(localName)
	btnDir, err := config.ButtonDir(slug)
	if err != nil {
		return nil, err
	}
	drawerDir, err := config.DrawerDir(slug)
	if err != nil {
		return nil, err
	}
	buttonSpec := filepath.Join(btnDir, "button.json")
	drawerSpec := filepath.Join(drawerDir, "drawer.json")
	buttonExists := fileExists(buttonSpec)
	drawerExists := fileExists(drawerSpec)
	switch {
	case buttonExists && drawerExists:
		return nil, fmt.Errorf("ambiguous package %q: found both button and drawer; rename one", slug)
	case buttonExists:
		return loadLocalButtonBundle(slug, identity, btnDir)
	case drawerExists:
		return loadLocalDrawerBundle(slug, identity, drawerDir)
	default:
		return nil, fmt.Errorf("package %q not found: expected button.json or drawer.json", localName)
	}
}

func loadLocalButtonBundle(localName, identity, dir string) (*Bundle, error) {
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
	return &Bundle{Name: name, Kind: "button", Version: spec.Version, SHA256: hashFiles(files), Files: files}, nil
}

func loadLocalDrawerBundle(localName, identity, dir string) (*Bundle, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("drawer %q not found: %w", localName, err)
	}

	files := map[string][]byte{}
	for _, e := range entries {
		if e.IsDir() {
			continue // skip pressed/ — run history isn't part of the artifact
		}
		if e.Type()&os.ModeSymlink != 0 {
			continue // don't follow symlinks; they can point outside the drawer root
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name())) // #nosec G304 -- dir is config.DrawerDir + enumerated entry.
		if err != nil {
			return nil, err
		}
		files[e.Name()] = data
	}
	specData, ok := files["drawer.json"]
	if !ok {
		return nil, fmt.Errorf("drawer %q has no drawer.json", localName)
	}
	var spec drawer.Drawer
	if err := json.Unmarshal(specData, &spec); err != nil {
		return nil, fmt.Errorf("drawer %q: invalid drawer.json: %w", localName, err)
	}

	name := spec.Name
	if identity != "" {
		name = identity
	}
	return &Bundle{Name: name, Kind: "drawer", Version: spec.Version, SHA256: hashFiles(files), Files: files}, nil
}

func stampLocalBundleVersion(localName string, bundle *Bundle, version string) error {
	switch bundle.Kind {
	case "", "button":
		return stampLocalButtonBundleVersion(localName, bundle, version)
	case "drawer":
		return stampLocalDrawerBundleVersion(localName, bundle, version)
	default:
		return fmt.Errorf("unsupported package kind %q", bundle.Kind)
	}
}

func stampLocalButtonBundleVersion(localName string, bundle *Bundle, version string) error {
	specData, ok := bundle.Files["button.json"]
	if !ok {
		return fmt.Errorf("button %q has no button.json", localName)
	}
	var spec button.Button
	if err := json.Unmarshal(specData, &spec); err != nil {
		return fmt.Errorf("button %q: invalid button.json: %w", localName, err)
	}
	spec.Version = version
	data, err := json.MarshalIndent(&spec, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %q button.json: %w", localName, err)
	}

	bundle.Version = version
	bundle.Files["button.json"] = data
	bundle.SHA256 = hashFiles(bundle.Files)

	dir, err := config.ButtonDir(button.Slugify(localName))
	if err != nil {
		return err
	}
	// #nosec G306 -- button.json is user-owned metadata in the configured button dir.
	if err := os.WriteFile(filepath.Join(dir, "button.json"), data, 0o600); err != nil {
		return fmt.Errorf("write %q button.json: %w", localName, err)
	}
	return nil
}

func stampLocalDrawerBundleVersion(localName string, bundle *Bundle, version string) error {
	specData, ok := bundle.Files["drawer.json"]
	if !ok {
		return fmt.Errorf("drawer %q has no drawer.json", localName)
	}
	var spec drawer.Drawer
	if err := json.Unmarshal(specData, &spec); err != nil {
		return fmt.Errorf("drawer %q: invalid drawer.json: %w", localName, err)
	}
	spec.Version = version
	data, err := json.MarshalIndent(&spec, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %q drawer.json: %w", localName, err)
	}

	bundle.Version = version
	bundle.Files["drawer.json"] = data
	bundle.SHA256 = hashFiles(bundle.Files)

	dir, err := config.DrawerDir(button.Slugify(localName))
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "drawer.json"), data, 0o600); err != nil {
		return fmt.Errorf("write %q drawer.json: %w", localName, err)
	}
	return nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
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
