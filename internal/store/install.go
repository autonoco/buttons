package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/drawer"
	"github.com/autonoco/buttons/internal/manifest"
)

type Result struct {
	Installed []string `json:"installed"`
}

type InstallOptions struct {
	// RefreshFloating resolves "latest" dependencies against the registry even
	// when the lock already has a version. `buttons add` and `buttons update`
	// set this; plain `buttons install` leaves it false to honor the lock.
	RefreshFloating bool
	Now             func() time.Time
}

func InstallManifest(src Source, m *manifest.Manifest, prior *manifest.Lockfile, opts InstallOptions) (*Result, *manifest.Lockfile, error) {
	if m == nil {
		return nil, nil, fmt.Errorf("manifest is required")
	}
	if err := m.Validate(); err != nil {
		return nil, nil, err
	}
	if prior == nil {
		prior = manifest.NewLockfile()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	next := manifest.NewLockfile()
	seen := map[string]bool{}
	installedNames := map[string]string{}
	var installed []string

	names := make([]string, 0, len(m.Dependencies))
	for name := range m.Dependencies {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := installPackage(src, name, m.Dependencies[name], prior, next, seen, installedNames, opts, &installed); err != nil {
			return nil, nil, err
		}
	}
	return &Result{Installed: installed}, next, nil
}

func installPackage(src Source, name, requested string, prior, next *manifest.Lockfile, seen map[string]bool, installedNames map[string]string, opts InstallOptions, out *[]string) error {
	if seen[name] {
		return nil
	}
	seen[name] = true

	if err := manifest.ValidatePackageName(name); err != nil {
		return err
	}
	if err := manifest.ValidateRequest(requested); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}

	version := requested
	if manifest.IsFloating(requested) {
		version = ""
		if !opts.RefreshFloating {
			if locked, ok := prior.Dependencies[name]; ok && locked.Requested == requested && locked.Version != "" {
				version = locked.Version
			}
		}
	}
	ref, err := Resolve(src, name, version)
	if err != nil {
		return err
	}
	if ref.Kind == "" {
		ref.Kind = "button"
	}
	switch ref.Kind {
	case "button":
		return installButtonPackage(src, name, requested, ref, prior, next, seen, installedNames, opts, out)
	case "drawer":
		return installDrawerPackage(src, name, requested, ref, prior, next, seen, installedNames, opts, out)
	default:
		return fmt.Errorf("installing %s kind %q is not implemented", name, ref.Kind)
	}
}

func installButtonPackage(src Source, name, requested string, ref ButtonRef, prior, next *manifest.Lockfile, seen map[string]bool, installedNames map[string]string, opts InstallOptions, out *[]string) error {
	bundle, spec, err := fetchButtonInstallable(src, name, ref.Version)
	if err != nil {
		return err
	}
	localName := button.Slugify(spec.Name)
	if owner, exists := installedNames[localName]; exists && owner != name {
		return fmt.Errorf("dependencies %s and %s both install as %q", owner, name, localName)
	}
	installedNames[localName] = name

	if err := writeBundle(spec, bundle); err != nil {
		return err
	}
	*out = append(*out, spec.Name)
	next.Dependencies[name] = manifest.LockEntry{
		Kind:          ref.Kind,
		Requested:     requested,
		Version:       bundle.Version,
		ContentHash:   bundle.SHA256,
		InstalledName: spec.Name,
		ResolvedAt:    opts.Now().UTC().Format(time.RFC3339),
	}

	deps := make([]string, 0, len(spec.Requires))
	for dep := range spec.Requires {
		deps = append(deps, dep)
	}
	sort.Strings(deps)
	for _, dep := range deps {
		if err := installPackage(src, dep, spec.Requires[dep], prior, next, seen, installedNames, opts, out); err != nil {
			return fmt.Errorf("dependency of %q: %w", spec.Name, err)
		}
	}
	return nil
}

func installDrawerPackage(src Source, name, requested string, ref ButtonRef, prior, next *manifest.Lockfile, seen map[string]bool, installedNames map[string]string, opts InstallOptions, out *[]string) error {
	bundle, spec, err := fetchDrawerInstallable(src, name, ref.Version)
	if err != nil {
		return err
	}
	localName := button.Slugify(spec.Name)
	if owner, exists := installedNames[localName]; exists && owner != name {
		return fmt.Errorf("dependencies %s and %s both install as %q", owner, name, localName)
	}
	installedNames[localName] = name

	if err := writeDrawerBundle(spec, bundle); err != nil {
		return err
	}
	*out = append(*out, spec.Name)
	next.Dependencies[name] = manifest.LockEntry{
		Kind:          ref.Kind,
		Requested:     requested,
		Version:       bundle.Version,
		ContentHash:   bundle.SHA256,
		InstalledName: spec.Name,
		ResolvedAt:    opts.Now().UTC().Format(time.RFC3339),
	}

	deps, err := drawerPackageDependencies(name, spec)
	if err != nil {
		return err
	}
	for _, dep := range deps {
		if err := installPackage(src, dep, "latest", prior, next, seen, installedNames, opts, out); err != nil {
			return fmt.Errorf("dependency of drawer %q: %w", spec.Name, err)
		}
	}
	return nil
}

func Resolve(src Source, name, version string) (ButtonRef, error) {
	refs, err := src.Index()
	if err != nil {
		return ButtonRef{}, err
	}
	var latest *ButtonRef
	for i := range refs {
		ref := refs[i]
		if ref.Name != name {
			continue
		}
		if ref.Kind == "" {
			ref.Kind = "button"
		}
		if version != "" && ref.Version == version {
			return ref, nil
		}
		if version == "" {
			latest = &ref
		}
	}
	if version != "" {
		return ButtonRef{}, fmt.Errorf("package %q@%s not found in registry", name, version)
	}
	if latest == nil {
		return ButtonRef{}, fmt.Errorf("package %q not found in registry", name)
	}
	return *latest, nil
}

func fetchButtonInstallable(src Source, name, version string) (*Bundle, *button.Button, error) {
	bundle, err := src.Fetch(name, version)
	if err != nil {
		return nil, nil, err
	}
	if _, ok := bundle.Files["button.json"]; !ok {
		return nil, nil, fmt.Errorf("button %q: bundle has no button.json", name)
	}
	var spec button.Button
	if err := json.Unmarshal(bundle.Files["button.json"], &spec); err != nil {
		return nil, nil, fmt.Errorf("button %q: invalid button.json: %w", name, err)
	}
	spec.Version = bundle.Version
	stamped, err := json.MarshalIndent(&spec, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	bundle.Files["button.json"] = append(stamped, '\n')
	return bundle, &spec, nil
}

func fetchDrawerInstallable(src Source, name, version string) (*Bundle, *drawer.Drawer, error) {
	bundle, err := src.Fetch(name, version)
	if err != nil {
		return nil, nil, err
	}
	if _, ok := bundle.Files["drawer.json"]; !ok {
		return nil, nil, fmt.Errorf("drawer %q: bundle has no drawer.json", name)
	}
	var spec drawer.Drawer
	if err := json.Unmarshal(bundle.Files["drawer.json"], &spec); err != nil {
		return nil, nil, fmt.Errorf("drawer %q: invalid drawer.json: %w", name, err)
	}
	spec.Version = bundle.Version
	stamped, err := json.MarshalIndent(&spec, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	bundle.Files["drawer.json"] = append(stamped, '\n')
	return bundle, &spec, nil
}

func writeBundle(spec *button.Button, bundle *Bundle) error {
	dir, err := config.ButtonDir(button.Slugify(spec.Name))
	if err != nil {
		return err
	}
	dsts := make(map[string]string, len(bundle.Files))
	for rel := range bundle.Files {
		dst, err := safeJoin(dir, rel)
		if err != nil {
			return err
		}
		dsts[rel] = dst
	}
	if err := os.MkdirAll(filepath.Join(dir, "pressed"), 0o700); err != nil {
		return fmt.Errorf("create button dir: %w", err)
	}
	for rel, data := range bundle.Files {
		dst := dsts[rel]
		mode := os.FileMode(0o600)
		if strings.HasPrefix(rel, "main.") {
			mode = 0o700
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return fmt.Errorf("create parent for %s: %w", rel, err)
		}
		if err := os.WriteFile(dst, data, mode); err != nil {
			return fmt.Errorf("write %s: %w", rel, err)
		}
	}
	if err := writeInstallState(dir, bundle.SHA256); err != nil {
		return fmt.Errorf("write install state: %w", err)
	}
	return nil
}

func writeDrawerBundle(spec *drawer.Drawer, bundle *Bundle) error {
	dir, err := config.DrawerDir(button.Slugify(spec.Name))
	if err != nil {
		return err
	}
	dsts := make(map[string]string, len(bundle.Files))
	for rel := range bundle.Files {
		dst, err := safeJoin(dir, rel)
		if err != nil {
			return err
		}
		dsts[rel] = dst
	}
	if err := os.MkdirAll(filepath.Join(dir, "pressed"), 0o700); err != nil {
		return fmt.Errorf("create drawer dir: %w", err)
	}
	for rel, data := range bundle.Files {
		dst := dsts[rel]
		mode := os.FileMode(0o700)
		if filepath.Ext(rel) == ".json" {
			mode = 0o600
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return fmt.Errorf("create parent for %s: %w", rel, err)
		}
		if err := os.WriteFile(dst, data, mode); err != nil {
			return fmt.Errorf("write %s: %w", rel, err)
		}
	}
	if err := writeInstallState(dir, bundle.SHA256); err != nil {
		return fmt.Errorf("write install state: %w", err)
	}
	return nil
}

func drawerPackageDependencies(packageName string, spec *drawer.Drawer) ([]string, error) {
	desk, _, err := splitScoped(packageName)
	if err != nil {
		return nil, err
	}
	deps := map[string]bool{}
	var walk func([]drawer.Step) error
	walk = func(steps []drawer.Step) error {
		for _, st := range steps {
			kind := st.Kind
			if kind == "" {
				kind = "button"
			}
			switch kind {
			case "button":
				if st.Button != "" {
					dep, err := scopedPackageForLocal(desk, st.Button)
					if err != nil {
						return err
					}
					deps[dep] = true
				}
			case "drawer":
				if st.Drawer != "" {
					dep, err := scopedPackageForLocal(desk, st.Drawer)
					if err != nil {
						return err
					}
					deps[dep] = true
				}
			}
			if len(st.Steps) > 0 {
				if err := walk(st.Steps); err != nil {
					return err
				}
			}
			for _, c := range st.Cases {
				if err := walk(c.Steps); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(spec.Steps); err != nil {
		return nil, err
	}
	if spec.OnError != nil && spec.OnError.Drawer != "" {
		dep, err := scopedPackageForLocal(desk, spec.OnError.Drawer)
		if err != nil {
			return nil, err
		}
		deps[dep] = true
	}
	out := make([]string, 0, len(deps))
	for dep := range deps {
		if dep == packageName {
			continue
		}
		out = append(out, dep)
	}
	sort.Strings(out)
	return out, nil
}

func scopedPackageForLocal(desk, name string) (string, error) {
	local := button.Slugify(name)
	if local == "" {
		return "", fmt.Errorf("drawer step target %q is empty after slugification", name)
	}
	pkg := desk + "/" + local
	if err := manifest.ValidatePackageName(pkg); err != nil {
		return "", err
	}
	return pkg, nil
}
