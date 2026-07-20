package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/drawer"
	"github.com/autonoco/buttons/internal/manifest"
)

// UninstallResult reports what Uninstall changed on disk.
type UninstallResult struct {
	// Removed lists installed names whose materialized dirs were deleted.
	Removed []string `json:"removed"`
	// Kept lists installed names left on disk — either another locked
	// package still requires them, or the dir has no install state (it was
	// not materialized by the installer, so we refuse to delete it).
	Kept []string `json:"kept,omitempty"`
}

// Uninstall reverses what InstallManifest materialized for one package: it
// deletes the package's button/drawer dir and drops its lock entry. It only
// deletes dirs stamped with install state (written by the installer) — a dir
// without one is never touched. A package another locked package still
// requires keeps both its files and its lock entry; it remains a live
// transitive dependency until its last dependent is removed. Transitive
// dependencies of the removed package are not cascaded; the next full
// install rebuilds the lock from the manifest and drops orphaned entries.
func Uninstall(lock *manifest.Lockfile, name string) (*UninstallResult, error) {
	if lock == nil {
		return nil, fmt.Errorf("lockfile is required")
	}
	if err := manifest.ValidatePackageName(name); err != nil {
		return nil, err
	}
	res := &UninstallResult{Removed: []string{}}
	entry, ok := lock.Dependencies[name]
	if !ok {
		return res, nil // never installed; nothing materialized to delete
	}
	if dependents := lockDependents(lock, name); len(dependents) > 0 {
		res.Kept = append(res.Kept, entry.InstalledName)
		return res, nil
	}
	dir, err := packageDir(entry.Kind, button.Slugify(entry.InstalledName))
	if err != nil {
		return nil, err
	}
	delete(lock.Dependencies, name)
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return res, nil // nothing materialized on disk
		}
		return nil, fmt.Errorf("stat %s: %w", dir, err)
	}
	if _, err := ReadInstallState(dir); err != nil {
		// No install state means the installer did not materialize this
		// dir — never delete files install state does not record.
		res.Kept = append(res.Kept, entry.InstalledName)
		return res, nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return nil, fmt.Errorf("remove %s: %w", dir, err)
	}
	res.Removed = append(res.Removed, entry.InstalledName)
	return res, nil
}

func packageDir(kind, localName string) (string, error) {
	switch kind {
	case "button":
		return config.ButtonDir(localName)
	case "drawer":
		return config.DrawerDir(localName)
	default:
		return "", fmt.Errorf("uninstalling kind %q is not implemented", kind)
	}
}

// lockDependents returns the other locked packages that still require pkg,
// read from their installed specs on disk. Best-effort: a package whose
// installed spec is missing or unreadable contributes no requirements.
func lockDependents(lock *manifest.Lockfile, pkg string) []string {
	names := make([]string, 0, len(lock.Dependencies))
	for name := range lock.Dependencies {
		names = append(names, name)
	}
	sort.Strings(names)
	var dependents []string
	for _, name := range names {
		if name == pkg {
			continue
		}
		for _, dep := range installedRequires(name, lock.Dependencies[name]) {
			if dep == pkg {
				dependents = append(dependents, name)
				break
			}
		}
	}
	return dependents
}

// installedRequires returns the package dependencies recorded in the
// installed spec (button.json / drawer.json) of one locked package.
func installedRequires(name string, entry manifest.LockEntry) []string {
	dir, err := packageDir(entry.Kind, button.Slugify(entry.InstalledName))
	if err != nil {
		return nil
	}
	switch entry.Kind {
	case "button":
		data, err := os.ReadFile(filepath.Join(dir, "button.json")) // #nosec G304 -- dir is resolved under the Buttons data dir.
		if err != nil {
			return nil
		}
		var spec button.Button
		if err := json.Unmarshal(data, &spec); err != nil {
			return nil
		}
		deps := make([]string, 0, len(spec.Requires))
		for dep := range spec.Requires {
			deps = append(deps, dep)
		}
		sort.Strings(deps)
		return deps
	case "drawer":
		data, err := os.ReadFile(filepath.Join(dir, "drawer.json")) // #nosec G304 -- dir is resolved under the Buttons data dir.
		if err != nil {
			return nil
		}
		var spec drawer.Drawer
		if err := json.Unmarshal(data, &spec); err != nil {
			return nil
		}
		deps, err := drawerPackageDependencies(name, &spec)
		if err != nil {
			return nil
		}
		return deps
	}
	return nil
}
